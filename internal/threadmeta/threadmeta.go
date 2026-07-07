// Package threadmeta upserts AG-UI thread metadata for in-process callers that
// bypass the gRPC transport (scheduler cron ticks, other server-side ADK runs).
//
// AGUI's ThreadService authorizer looks up the caller identity from the gRPC
// "x-alis-identity" incoming metadata. When a launcher calls the service in
// process — without going through a gRPC client stub — that metadata is
// missing, and the authorizer rejects the call as unauthenticated. This
// package injects the required metadata (and a matching server transport
// stream so the authorizer can resolve the method) before invoking the
// service directly.
//
// The pattern mirrors and is intentionally kept in sync with the equivalent
// helpers in package go.alis.build/adk/launchers/agui (see agui/threads.go).
// The two implementations share the same injection strategy so scheduler cron
// runs and AGUI interactive runs are indistinguishable to the ThreadService
// authorizer.
//
// This package is internal: it exists to keep the injection logic and the
// cron app-name resolution rules out of the scheduler package's public API.
// External callers should use the agui/scheduler launchers directly.
package threadmeta

import (
	"context"
	"fmt"
	"log"
	"slices"
	"strings"

	historyservice "go.alis.build/agui/history/service"
	pb "go.alis.build/common/alis/agui/history/v1"
	"go.alis.build/iam/v3"
	adklauncher "google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// ThreadIDFromResource returns the thread id segment from a threads/{thread_id}
// resource name. Returns the empty string when the input does not have the
// "threads/" prefix (including when it is already an unqualified id).
func ThreadIDFromResource(thread string) string {
	thread = strings.TrimSpace(thread)
	id, ok := strings.CutPrefix(thread, "threads/")
	if !ok {
		return ""
	}
	return strings.TrimSpace(id)
}

// ThreadResource formats an ADK session id as a threads/{session_id} resource
// name. Returns the empty string for an empty or whitespace-only input so
// callers can safely skip the "thread" field on Cron updates when no session
// exists yet.
func ThreadResource(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return ""
	}
	return "threads/" + sessionID
}

// Upsert creates or updates thread metadata via ThreadService for an
// in-process caller. It resolves the agent display name from cfg.AgentLoader
// (falling back to launcherDefault) and injects the caller identity into gRPC
// metadata so the ThreadService authorizer accepts the call.
//
// Best-effort: a nil svc or blank threadID is a no-op; upstream failures are
// logged and swallowed so a metadata write never fails a cron tick. Safe for
// concurrent use as long as svc is.
func Upsert(
	ctx context.Context,
	svc *historyservice.ThreadService,
	identity *iam.Identity,
	cfg *adklauncher.Config,
	launcherDefault, threadID, agentID, userMessageText string,
) {
	if svc == nil {
		return
	}
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return
	}

	svcCtx := injectGrpcMetadata(ctx, identity, pb.ThreadService_GetThread_FullMethodName)
	displayName := agentDisplayName(cfg, launcherDefault, agentID)
	if err := svc.CreateOrUpdateThread(svcCtx, &historyservice.CreateOrUpdateThreadRequest{
		ThreadID:         threadID,
		AgentID:          agentID,
		AgentDisplayName: displayName,
		UserMessageText:  userMessageText,
	}); err != nil {
		log.Printf("threadmeta: upsert failed for %s: %v", threadID, err)
	}
}

func agentDisplayName(cfg *adklauncher.Config, launcherDefault, agentID string) string {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return strings.TrimSpace(launcherDefault)
	}
	if cfg != nil && cfg.AgentLoader != nil {
		if ag, err := cfg.AgentLoader.LoadAgent(agentID); err == nil && ag != nil {
			if desc := strings.TrimSpace(ag.Description()); desc != "" {
				return desc
			}
		}
		agents := cfg.AgentLoader.ListAgents()
		if len(agents) == 1 && agents[0] == agentID {
			if name := strings.TrimSpace(launcherDefault); name != "" {
				return name
			}
		}
	}
	return agentID
}

func injectGrpcMetadata(ctx context.Context, identity *iam.Identity, method string) context.Context {
	if identity == nil {
		return ctx
	}
	md := metadata.MD{
		"x-alis-identity": {string(identity.Marshal())},
	}
	ctx = grpc.NewContextWithServerTransportStream(ctx, &grpcMethodStream{
		method: fmt.Sprintf("/%s/%s", pb.ThreadService_ServiceDesc.ServiceName, extractMethodName(method)),
	})
	return metadata.NewIncomingContext(ctx, md)
}

func extractMethodName(fullMethod string) string {
	parts := strings.Split(fullMethod, "/")
	return parts[len(parts)-1]
}

type grpcMethodStream struct {
	method string
}

func (s *grpcMethodStream) Method() string                 { return s.method }
func (s *grpcMethodStream) SetHeader(_ metadata.MD) error  { return nil }
func (s *grpcMethodStream) SendHeader(_ metadata.MD) error { return nil }
func (s *grpcMethodStream) SetTrailer(_ metadata.MD) error { return nil }

// ResolveCronAppName returns the ADK app name to run for a cron. When
// cron.agent_id is set (after trimming), it wins; otherwise defaultAppName
// (the launcher-wide default from [scheduler.NewLauncher]) is returned.
// Both inputs are whitespace-trimmed before comparison.
func ResolveCronAppName(agentID, defaultAppName string) string {
	if id := strings.TrimSpace(agentID); id != "" {
		return id
	}
	return strings.TrimSpace(defaultAppName)
}

// ValidateCronAppName rejects a cron's resolved app name when it is not known
// to cfg.AgentLoader. Returns an error for an empty name, or for a name that
// is not in cfg.AgentLoader.ListAgents() when the loader reports a non-empty
// set. A nil cfg, nil AgentLoader, or empty agent list means "not configured
// to validate" and the check is skipped (any non-empty name passes).
func ValidateCronAppName(cfg *adklauncher.Config, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("app name is required")
	}
	if cfg == nil || cfg.AgentLoader == nil {
		return nil
	}
	agents := cfg.AgentLoader.ListAgents()
	if len(agents) == 0 {
		return nil
	}
	if !slices.Contains(agents, name) {
		return fmt.Errorf("unknown agent %q", name)
	}
	return nil
}
