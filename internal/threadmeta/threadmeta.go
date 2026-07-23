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
// This package is internal: it exists to keep the injection logic out of the
// scheduler package's public API. External callers should use the agui and
// scheduler launchers directly.
package threadmeta

import (
	"context"
	"fmt"
	"log"
	"strings"

	historyservice "go.alis.build/agui/history/service"
	pb "go.alis.build/common/alis/agui/history/v1"
	"go.alis.build/iam/v3"
	adklauncher "google.golang.org/adk/cmd/launcher"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

// Upserter is the subset of [historyservice.ThreadService] used for in-process
// metadata writes. Satisfied by *historyservice.ThreadService and test doubles.
type Upserter interface {
	CreateOrUpdateThread(ctx context.Context, req *historyservice.CreateOrUpdateThreadRequest) error
}

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

// ThreadIDFromSession returns the ADK session id used as an AG-UI thread id.
// Accepts either a bare session id (A2A scheduler context_id) or a
// threads/{id} resource name.
func ThreadIDFromSession(session string) string {
	if id := ThreadIDFromResource(session); id != "" {
		return id
	}
	return strings.TrimSpace(session)
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
	svc Upserter,
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
