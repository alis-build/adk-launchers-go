package evals

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"go.alis.build/adk/launchers/evals/evaluation/generator"
	"go.alis.build/adk/launchers/evals/evaluation/metrics"
	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/service"
	"go.alis.build/adk/launchers/evals/evaluation/simulation"
	"go.alis.build/adk/launchers/evals/evaluation/storage"
	"go.alis.build/adk/launchers/internal/adkrun"
	"go.alis.build/adk/launchers/internal/launcherutils"
	_ "go.alis.build/adk/launchers/internal/testenv"
	launchersweb "go.alis.build/adk/launchers/web"
	alismux "go.alis.build/mux"
	adklauncher "google.golang.org/adk/cmd/launcher"
	adkweb "google.golang.org/adk/cmd/launcher/web"
	"google.golang.org/adk/session"
)

// Launcher is the public surface of [NewLauncher].
type Launcher interface {
	adkweb.Sublauncher
	launchersweb.HostRouteSetup
}

// Option configures optional [evalsLauncher] settings.
type Option func(*evalsLauncher)

// WithAgentsDir sets the directory containing agent apps for local eval storage.
func WithAgentsDir(dir string) Option {
	return func(l *evalsLauncher) {
		l.agentsDir = dir
	}
}

// WithEvalStorageURI uses GCS for eval sets and results (gs://bucket).
func WithEvalStorageURI(uri string) Option {
	return func(l *evalsLauncher) {
		l.evalStorageURI = uri
	}
}

// WithEvalSetsManager overrides the default eval sets storage backend.
func WithEvalSetsManager(m storage.EvalSetsManager) Option {
	return func(l *evalsLauncher) {
		l.setsManager = m
	}
}

// WithEvalSetResultsManager overrides the default eval results storage backend.
func WithEvalSetResultsManager(m storage.EvalSetResultsManager) Option {
	return func(l *evalsLauncher) {
		l.resultsManager = m
	}
}

// WithMetricRegistry sets the metric registry used for evaluation scoring.
func WithMetricRegistry(r *metrics.Registry) Option {
	return func(l *evalsLauncher) {
		l.registry = r
	}
}

// WithUserSimulatorProvider configures user simulator selection during inference.
func WithUserSimulatorProvider(p simulation.UserSimulatorProvider) Option {
	return func(l *evalsLauncher) {
		l.simProvider = p
	}
}

// WithPathPrefix sets the HTTP path prefix before /dev/apps/... (default "/api").
// Match the webui -api_server_address and api sublauncher -path_prefix.
func WithPathPrefix(prefix string) Option {
	return func(l *evalsLauncher) {
		l.pathPrefix = normalizePathPrefix(prefix)
	}
}

// evalsLauncher implements the evals sublauncher and mounts dev eval routes on
// the host mux via HostRouteSetup. adkrun.Runtime instances are cached per app
// name for the lifetime of the launcher (intended for local dev servers).
type evalsLauncher struct {
	flags          *flag.FlagSet
	pathPrefix     string
	agentsDir      string
	evalStorageURI string

	setsManager    storage.EvalSetsManager
	resultsManager storage.EvalSetResultsManager
	registry       *metrics.Registry
	simProvider    simulation.UserSimulatorProvider

	launcherCfg *adklauncher.Config
	runtimeMu   sync.Mutex
	runtimes    map[string]*adkrun.Runtime

	setupOnce sync.Once
	setupErr  error
}

var (
	_ Launcher                    = (*evalsLauncher)(nil)
	_ adkweb.Sublauncher          = (*evalsLauncher)(nil)
	_ launchersweb.HostRouteSetup = (*evalsLauncher)(nil)
)

// NewLauncher returns an evals sublauncher (CLI keyword "evals") that registers
// DevServer-equivalent HTTP routes on the host mux. Local storage requires
// [WithAgentsDir] or --agents_dir unless [WithEvalStorageURI] is set.
func NewLauncher(opts ...Option) Launcher {
	l := &evalsLauncher{
		pathPrefix:  "/api",
		registry:    metrics.DefaultRegistry,
		simProvider: simulation.UserSimulatorProvider{},
		runtimes:    make(map[string]*adkrun.Runtime),
	}
	for _, opt := range opts {
		opt(l)
	}

	fs := flag.NewFlagSet("evals", flag.ContinueOnError)
	fs.StringVar(&l.pathPrefix, "path_prefix", l.pathPrefix, "HTTP path prefix before /dev/apps (default /api, match webui api_server_address)")
	fs.StringVar(&l.agentsDir, "agents_dir", l.agentsDir, "Directory containing agent apps for local eval storage")
	fs.StringVar(&l.evalStorageURI, "eval_storage_uri", l.evalStorageURI, "GCS URI for eval storage (gs://bucket)")
	l.flags = fs
	return l
}

func (l *evalsLauncher) Keyword() string { return "evals" }

func (l *evalsLauncher) Parse(args []string) ([]string, error) {
	if err := l.flags.Parse(args); err != nil || !l.flags.Parsed() {
		return nil, fmt.Errorf("evals: parse flags: %w", err)
	}
	l.pathPrefix = normalizePathPrefix(l.pathPrefix)
	return l.flags.Args(), nil
}

// normalizePathPrefix ensures a leading slash and no trailing slash for route mounting.
func normalizePathPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return ""
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	return strings.TrimSuffix(prefix, "/")
}

func (l *evalsLauncher) CommandLineSyntax() string {
	return launcherutils.FormatFlagUsage(l.flags)
}

func (l *evalsLauncher) SimpleDescription() string {
	return "dev evaluation HTTP API (eval sets, run eval, metrics-info)"
}

func (l *evalsLauncher) SetupSubrouters(_ *mux.Router, _ *adklauncher.Config) error {
	return nil
}

func (l *evalsLauncher) SetupHostRoutes(config *adklauncher.Config) error {
	l.setupOnce.Do(func() {
		l.setupErr = l.mountHostRoutes(config)
	})
	return l.setupErr
}

// mountHostRoutes registers canonical and legacy eval HTTP routes on the host mux.
func (l *evalsLauncher) mountHostRoutes(config *adklauncher.Config) error {
	if config == nil {
		return fmt.Errorf("evals: launcher config is required")
	}
	if config.SessionService == nil {
		return fmt.Errorf("evals: session service is required")
	}
	if config.AgentLoader == nil {
		return fmt.Errorf("evals: agent loader is required")
	}
	l.launcherCfg = config

	if err := l.initStorage(config); err != nil {
		return err
	}

	devApp := l.devAppBase()

	alismux.Post(devApp+"/eval-sets", l.createEvalSetHandler())
	alismux.Get(devApp+"/eval-sets", l.listEvalSetsHandler())
	alismux.Get(devApp+"/eval-sets/{eval_set_id}", l.getEvalSetHandler())
	alismux.Delete(devApp+"/eval-sets/{eval_set_id}", l.deleteEvalSetHandler())
	alismux.Post(devApp+"/eval-sets/{eval_set_id}/add-session", l.addSessionHandler())
	alismux.Post(devApp+"/eval-sets/{eval_set_id}/run", l.runEvalHandler())
	alismux.Get(devApp+"/eval-sets/{eval_set_id}/eval-cases", l.listEvalCasesHandler())
	alismux.Get(devApp+"/eval-sets/{eval_set_id}/eval-cases/{eval_case_id}", l.getEvalCaseHandler())
	alismux.Put(devApp+"/eval-sets/{eval_set_id}/eval-cases/{eval_case_id}", l.updateEvalCaseHandler())
	alismux.Delete(devApp+"/eval-sets/{eval_set_id}/eval-cases/{eval_case_id}", l.deleteEvalCaseHandler())

	l.mountLegacyEvalRoutes(devApp)
	// Bundled adk-web calls legacy underscore paths at {pathPrefix}/apps/... (no /dev).
	l.mountLegacyEvalRoutes(l.webuiAppBase())

	alismux.Get(devApp+"/eval-results", l.listEvalResultsHandler())
	alismux.Get(devApp+"/eval-results/{eval_result_id}", l.getEvalResultHandler())

	alismux.Get(devApp+"/metrics-info", l.metricsInfoHandler())
	return nil
}

// mountLegacyEvalRoutes registers underscore-named eval routes for adk-web / DevServer parity.
func (l *evalsLauncher) mountLegacyEvalRoutes(base string) {
	alismux.Post(base+"/eval_sets/{eval_set_id}", l.createEvalSetLegacyHandler())
	alismux.Get(base+"/eval_sets", l.listEvalSetsLegacyHandler())
	alismux.Get(base+"/eval_sets/{eval_set_id}", l.getEvalSetHandler())
	alismux.Delete(base+"/eval_sets/{eval_set_id}", l.deleteEvalSetHandler())
	alismux.Post(base+"/eval_sets/{eval_set_id}/add_session", l.addSessionHandler())
	alismux.Post(base+"/eval_sets/{eval_set_id}/run_eval", l.runEvalLegacyHandler())
	alismux.Get(base+"/eval_sets/{eval_set_id}/evals", l.listEvalCasesHandler())
	alismux.Get(base+"/eval_sets/{eval_set_id}/evals/{eval_case_id}", l.getEvalCaseHandler())
	alismux.Put(base+"/eval_sets/{eval_set_id}/evals/{eval_case_id}", l.updateEvalCaseHandler())
	alismux.Delete(base+"/eval_sets/{eval_set_id}/evals/{eval_case_id}", l.deleteEvalCaseHandler())
	alismux.Get(base+"/eval_results", l.listEvalResultsLegacyHandler())
	alismux.Get(base+"/eval_results/{eval_result_id}", l.getEvalResultHandler())
}

// initStorage constructs eval set and result managers from options or defaults.
func (l *evalsLauncher) initStorage(_ *adklauncher.Config) error {
	if l.setsManager != nil && l.resultsManager != nil {
		return nil
	}
	if l.evalStorageURI != "" {
		bucket, err := bucketFromURI(l.evalStorageURI)
		if err != nil {
			return fmt.Errorf("evals: eval_storage_uri: %w", err)
		}
		// Bound GCS setup so a bad URI or unreachable metadata server cannot
		// hang launcher startup indefinitely.
		ctx, cancel := context.WithTimeout(context.Background(), gcsSetupTimeout)
		defer cancel()
		mgrs, err := storage.NewGCSManagers(ctx, bucket, nil)
		if err != nil {
			return fmt.Errorf("evals: gcs storage: %w", err)
		}
		if l.setsManager == nil {
			l.setsManager = mgrs.Sets
		}
		if l.resultsManager == nil {
			l.resultsManager = mgrs.Results
		}
		return nil
	}
	if l.agentsDir == "" {
		return fmt.Errorf("evals: agents_dir is required for local eval storage (use WithAgentsDir or --agents_dir)")
	}
	if l.setsManager == nil {
		l.setsManager = storage.NewLocalEvalSetsManager(l.agentsDir)
	}
	if l.resultsManager == nil {
		l.resultsManager = storage.NewLocalEvalSetResultsManager(l.agentsDir)
	}
	return nil
}

// gcsSetupTimeout bounds Attrs()/metadata calls during launcher startup so a
// wrong URI or unreachable network can't block adk web forever.
const gcsSetupTimeout = 15 * time.Second

// bucketFromURI extracts the bucket name from a gs://bucket[/path] URI and
// rejects any other scheme so a caller does not silently create objects under
// an unintended provider name (e.g. s3://foo → bucket named "s3://foo").
func bucketFromURI(uri string) (string, error) {
	trimmed := strings.TrimSpace(uri)
	if !strings.HasPrefix(trimmed, "gs://") {
		return "", fmt.Errorf("expected gs://bucket URI, got %q", uri)
	}
	rest := strings.TrimPrefix(trimmed, "gs://")
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	if rest == "" {
		return "", fmt.Errorf("gs:// URI missing bucket name: %q", uri)
	}
	return rest, nil
}

// devAppBase returns the path prefix for canonical per-app dev eval routes.
func (l *evalsLauncher) devAppBase() string {
	return l.pathPrefix + "/dev/apps/{app_name}"
}

// webuiAppBase returns the path prefix where bundled adk-web issues legacy eval requests.
func (l *evalsLauncher) webuiAppBase() string {
	return l.pathPrefix + "/apps/{app_name}"
}

func (l *evalsLauncher) UserMessage(webURL string, printer func(v ...any)) {
	printer(fmt.Sprintf("       evals:  webui eval API at %s%s/apps/{app}/eval_sets", webURL, l.pathPrefix))
	printer(fmt.Sprintf("       evals:  dev eval API at %s%s/dev/apps/{app}/eval-sets", webURL, l.pathPrefix))
}

// runtimeForApp returns a cached adkrun.Runtime for the given app name.
func (l *evalsLauncher) runtimeForApp(appName string) (*adkrun.Runtime, error) {
	l.runtimeMu.Lock()
	defer l.runtimeMu.Unlock()
	if rt, ok := l.runtimes[appName]; ok {
		return rt, nil
	}
	rt, err := adkrun.NewRuntime(l.launcherCfg, appName)
	if err != nil {
		return nil, err
	}
	l.runtimes[appName] = rt
	return rt, nil
}

// localEvalService wires generator, storage, registry, and session services for one app.
func (l *evalsLauncher) localEvalService(appName string) (*service.LocalEvalService, error) {
	rt, err := l.runtimeForApp(appName)
	if err != nil {
		return nil, err
	}
	return &service.LocalEvalService{
		Generator:   &generator.Generator{Runtime: rt},
		Sets:        l.setsManager,
		Results:     l.resultsManager,
		Registry:    l.registry,
		SimProvider: l.simProvider,
		Sessions:    l.launcherCfg.SessionService,
	}, nil
}

func (l *evalsLauncher) pathAppName(r *http.Request) (string, error) {
	appName := r.PathValue("app_name")
	if err := storage.ValidatePathSegment(appName, "app_name"); err != nil {
		return "", alismux.BadRequestErr("%s", err.Error())
	}
	return appName, nil
}

func (l *evalsLauncher) pathEvalSetID(r *http.Request) (string, error) {
	id := r.PathValue("eval_set_id")
	if err := storage.ValidateEvalSetID(id); err != nil {
		return "", alismux.BadRequestErr("%s", err.Error())
	}
	return id, nil
}

func (l *evalsLauncher) pathEvalCaseID(r *http.Request) (string, error) {
	id := r.PathValue("eval_case_id")
	if err := storage.ValidatePathSegment(id, "eval case id"); err != nil {
		return "", alismux.BadRequestErr("%s", err.Error())
	}
	return id, nil
}

// writeJSON sets Content-Type and encodes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	return json.NewEncoder(w).Encode(v)
}

// decodeJSON decodes the request body into dst.
func decodeJSON(r *http.Request, dst any) error {
	return json.NewDecoder(r.Body).Decode(dst)
}

// storageNotFound reports whether err is storage.ErrNotFound.
func storageNotFound(err error) bool {
	return errors.Is(err, storage.ErrNotFound)
}

func (l *evalsLauncher) createEvalSetHandler() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		appName, err := l.pathAppName(r)
		if err != nil {
			return err
		}
		var req CreateEvalSetRequest
		if err := decodeJSON(r, &req); err != nil {
			return alismux.BadRequestErr("invalid request body: %s", err.Error())
		}
		evalSetID := strings.TrimSpace(req.EvalSet.EvalSetID)
		if evalSetID == "" {
			return alismux.BadRequestErr("eval set id is required")
		}
		set, err := l.setsManager.CreateEvalSet(appName, evalSetID)
		if err != nil {
			return alismux.BadRequestErr("%s", err.Error())
		}
		if applyEvalSetMetadata(set, req.EvalSet) {
			if err := l.setsManager.UpdateEvalSet(appName, *set); err != nil {
				_ = l.setsManager.DeleteEvalSet(appName, evalSetID)
				return alismux.BadRequestErr("%s", err.Error())
			}
		}
		return writeJSON(w, http.StatusOK, set)
	}
}

// applyEvalSetMetadata copies caller-provided top-level metadata onto the
// storage-created eval set. It returns true when any field was applied so
// callers can decide whether to persist the update.
func applyEvalSetMetadata(dst *models.EvalSet, src models.EvalSet) bool {
	if dst == nil {
		return false
	}
	changed := false
	if src.ModelExecutionMode != nil {
		dst.ModelExecutionMode = src.ModelExecutionMode
		changed = true
	}
	if src.ToolExecutionMode != nil {
		dst.ToolExecutionMode = src.ToolExecutionMode
		changed = true
	}
	if src.Name != nil {
		dst.Name = src.Name
		changed = true
	}
	if src.Description != nil {
		dst.Description = src.Description
		changed = true
	}
	return changed
}

func (l *evalsLauncher) createEvalSetLegacyHandler() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		appName, err := l.pathAppName(r)
		if err != nil {
			return err
		}
		evalSetID, err := l.pathEvalSetID(r)
		if err != nil {
			return err
		}
		set, err := l.setsManager.CreateEvalSet(appName, evalSetID)
		if err != nil {
			return alismux.BadRequestErr("%s", err.Error())
		}
		return writeJSON(w, http.StatusOK, set)
	}
}

func (l *evalsLauncher) listEvalSetsHandler() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		appName, err := l.pathAppName(r)
		if err != nil {
			return err
		}
		ids, err := l.listEvalSetIDs(appName)
		if err != nil {
			return err
		}
		return writeJSON(w, http.StatusOK, ListEvalSetsResponse{EvalSetIDs: ids})
	}
}

func (l *evalsLauncher) listEvalSetsLegacyHandler() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		appName, err := l.pathAppName(r)
		if err != nil {
			return err
		}
		ids, err := l.listEvalSetIDs(appName)
		if err != nil {
			return err
		}
		return writeJSON(w, http.StatusOK, ids)
	}
}

// listEvalSetIDs returns eval set ids for an app, treating a missing app dir as empty.
func (l *evalsLauncher) listEvalSetIDs(appName string) ([]string, error) {
	ids, err := l.setsManager.ListEvalSets(appName)
	if err != nil && storageNotFound(err) {
		return []string{}, nil
	}
	return ids, err
}

func (l *evalsLauncher) getEvalSetHandler() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		appName, err := l.pathAppName(r)
		if err != nil {
			return err
		}
		evalSetID, err := l.pathEvalSetID(r)
		if err != nil {
			return err
		}
		set, err := l.setsManager.GetEvalSet(appName, evalSetID)
		if err != nil {
			return err
		}
		if set == nil {
			return alismux.NotFoundErr("eval set %q not found", evalSetID)
		}
		return writeJSON(w, http.StatusOK, set)
	}
}

func (l *evalsLauncher) deleteEvalSetHandler() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		appName, err := l.pathAppName(r)
		if err != nil {
			return err
		}
		evalSetID, err := l.pathEvalSetID(r)
		if err != nil {
			return err
		}
		if err := l.setsManager.DeleteEvalSet(appName, evalSetID); err != nil {
			if storageNotFound(err) {
				return alismux.NotFoundErr("%s", err.Error())
			}
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
}

func (l *evalsLauncher) listEvalCasesHandler() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		appName, err := l.pathAppName(r)
		if err != nil {
			return err
		}
		evalSetID, err := l.pathEvalSetID(r)
		if err != nil {
			return err
		}
		set, err := l.setsManager.GetEvalSet(appName, evalSetID)
		if err != nil {
			return err
		}
		if set == nil {
			return alismux.NotFoundErr("eval set %q not found", evalSetID)
		}
		ids := make([]string, 0, len(set.EvalCases))
		for _, c := range set.EvalCases {
			ids = append(ids, c.EvalID)
		}
		sort.Strings(ids)
		return writeJSON(w, http.StatusOK, ids)
	}
}

func (l *evalsLauncher) getEvalCaseHandler() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		appName, err := l.pathAppName(r)
		if err != nil {
			return err
		}
		evalSetID, err := l.pathEvalSetID(r)
		if err != nil {
			return err
		}
		evalCaseID, err := l.pathEvalCaseID(r)
		if err != nil {
			return err
		}
		evalCase, err := l.setsManager.GetEvalCase(appName, evalSetID, evalCaseID)
		if err != nil {
			return err
		}
		if evalCase == nil {
			return alismux.NotFoundErr("Eval set `%s` or Eval `%s` not found.", evalSetID, evalCaseID)
		}
		return writeJSON(w, http.StatusOK, evalCase)
	}
}

func (l *evalsLauncher) updateEvalCaseHandler() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		appName, err := l.pathAppName(r)
		if err != nil {
			return err
		}
		evalSetID, err := l.pathEvalSetID(r)
		if err != nil {
			return err
		}
		evalCaseID, err := l.pathEvalCaseID(r)
		if err != nil {
			return err
		}
		var updated models.EvalCase
		if err := json.NewDecoder(r.Body).Decode(&updated); err != nil {
			return alismux.BadRequestErr("invalid request body: %s", err.Error())
		}
		if updated.EvalID != "" && updated.EvalID != evalCaseID {
			return alismux.BadRequestErr("Eval id in EvalCase should match the eval id in the API route.")
		}
		updated.EvalID = evalCaseID
		if err := l.setsManager.UpdateEvalCase(appName, evalSetID, updated); err != nil {
			if storageNotFound(err) {
				return alismux.NotFoundErr("%s", err.Error())
			}
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
}

func (l *evalsLauncher) deleteEvalCaseHandler() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		appName, err := l.pathAppName(r)
		if err != nil {
			return err
		}
		evalSetID, err := l.pathEvalSetID(r)
		if err != nil {
			return err
		}
		evalCaseID, err := l.pathEvalCaseID(r)
		if err != nil {
			return err
		}
		if err := l.setsManager.DeleteEvalCase(appName, evalSetID, evalCaseID); err != nil {
			if storageNotFound(err) {
				return alismux.NotFoundErr("%s", err.Error())
			}
			return err
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
}

func (l *evalsLauncher) addSessionHandler() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		appName, err := l.pathAppName(r)
		if err != nil {
			return err
		}
		evalSetID, err := l.pathEvalSetID(r)
		if err != nil {
			return err
		}
		var req AddSessionToEvalSetRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return alismux.BadRequestErr("invalid request body: %s", err.Error())
		}
		if strings.TrimSpace(req.EvalID) == "" || strings.TrimSpace(req.SessionID) == "" || strings.TrimSpace(req.UserID) == "" {
			return alismux.BadRequestErr("evalId, sessionId, and userId are required")
		}

		resp, err := l.launcherCfg.SessionService.Get(r.Context(), &session.GetRequest{
			AppName:   appName,
			UserID:    req.UserID,
			SessionID: req.SessionID,
		})
		if err != nil {
			return alismux.BadRequestErr("session not found: %s", err.Error())
		}
		if resp == nil || resp.Session == nil {
			return alismux.BadRequestErr("session not found")
		}

		rootAgent, err := l.launcherCfg.AgentLoader.LoadAgent(appName)
		if err != nil {
			return alismux.BadRequestErr("agent not found: %s", err.Error())
		}

		invocations := generator.ConvertEventsToEvalInvocations(collectSessionEvents(resp.Session), nil)
		newCase := models.EvalCase{
			EvalID:       req.EvalID,
			Conversation: invocations,
			SessionInput: &models.SessionInput{
				AppName: appName,
				UserID:  req.UserID,
				State:   createEmptyState(rootAgent),
			},
			CreationTimestamp: float64(time.Now().Unix()),
		}
		if err := l.setsManager.AddEvalCase(appName, evalSetID, newCase); err != nil {
			return alismux.BadRequestErr("%s", err.Error())
		}
		w.WriteHeader(http.StatusNoContent)
		return nil
	}
}

// collectSessionEvents copies session events into a slice for conversion.
func collectSessionEvents(sess session.Session) []*session.Event {
	if sess == nil {
		return nil
	}
	evs := sess.Events()
	out := make([]*session.Event, 0, evs.Len())
	for i := 0; i < evs.Len(); i++ {
		out = append(out, evs.At(i))
	}
	return out
}

func (l *evalsLauncher) runEvalHandler() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		results, err := l.executeRunEval(r)
		if err != nil {
			return err
		}
		return writeJSON(w, http.StatusOK, RunEvalResponse{RunEvalResults: results})
	}
}

func (l *evalsLauncher) runEvalLegacyHandler() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		results, err := l.executeRunEval(r)
		if err != nil {
			return err
		}
		return writeJSON(w, http.StatusOK, results)
	}
}

// executeRunEval loads the eval set, runs inference and scoring, and returns results.
func (l *evalsLauncher) executeRunEval(r *http.Request) ([]models.RunEvalResult, error) {
	appName, err := l.pathAppName(r)
	if err != nil {
		return nil, err
	}
	evalSetID, err := l.pathEvalSetID(r)
	if err != nil {
		return nil, err
	}
	var req RunEvalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, alismux.BadRequestErr("invalid request body: %s", err.Error())
	}

	evalSet, err := l.setsManager.GetEvalSet(appName, evalSetID)
	if err != nil {
		return nil, err
	}
	if evalSet == nil {
		return nil, alismux.NotFoundErr("eval set %q not found", evalSetID)
	}

	svc, err := l.localEvalService(appName)
	if err != nil {
		return nil, alismux.BadRequestErr("evals: %s", err.Error())
	}

	caseIDs := req.EvalCaseIDs
	if len(caseIDs) == 0 {
		// Legacy run body uses eval_ids for case ids; accept either field.
		caseIDs = req.EvalIDs
	}

	inferences, err := svc.PerformInference(r.Context(), service.InferenceRequest{
		AppName:         appName,
		EvalSetID:       evalSetID,
		EvalCaseIDs:     caseIDs,
		InferenceConfig: service.InferenceConfig{},
	})
	if err != nil {
		return nil, alismux.BadRequestErr("%s", err.Error())
	}

	caseResults, evalErr := svc.Evaluate(r.Context(), service.EvaluateRequest{
		InferenceResults: inferences,
		EvaluateConfig: service.EvaluateConfig{
			EvalMetrics: req.EvalMetrics,
		},
	})
	// Return a hard error only when every case failed to evaluate; partial
	// results are still returned to the client (ADK Python parity).
	if len(caseResults) == 0 && evalErr != nil {
		return nil, alismux.BadRequestErr("%s", evalErr.Error())
	}

	out := make([]models.RunEvalResult, 0, len(caseResults))
	for _, cr := range caseResults {
		out = append(out, models.RunEvalResult{
			EvalSetFile:                   evalSetID,
			EvalSetID:                     evalSetID,
			EvalID:                        cr.EvalID,
			FinalEvalStatus:               cr.FinalEvalStatus,
			OverallEvalMetricResults:      cr.OverallEvalMetricResults,
			EvalMetricResultPerInvocation: cr.EvalMetricResultPerInvocation,
			UserID:                        cr.UserID,
			SessionID:                     cr.SessionID,
		})
	}
	return out, nil
}

func (l *evalsLauncher) listEvalResultsHandler() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		appName, err := l.pathAppName(r)
		if err != nil {
			return err
		}
		ids, err := l.resultsManager.ListEvalSetResults(appName)
		if err != nil {
			return err
		}
		return writeJSON(w, http.StatusOK, ListEvalResultsResponse{EvalResultIDs: ids})
	}
}

func (l *evalsLauncher) listEvalResultsLegacyHandler() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		appName, err := l.pathAppName(r)
		if err != nil {
			return err
		}
		ids, err := l.resultsManager.ListEvalSetResults(appName)
		if err != nil {
			return err
		}
		return writeJSON(w, http.StatusOK, ids)
	}
}

func (l *evalsLauncher) getEvalResultHandler() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		appName, err := l.pathAppName(r)
		if err != nil {
			return err
		}
		resultID := strings.TrimSpace(r.PathValue("eval_result_id"))
		if err := storage.ValidatePathSegment(resultID, "eval result id"); err != nil {
			return alismux.BadRequestErr("%s", err.Error())
		}
		result, err := l.resultsManager.GetEvalSetResult(appName, resultID)
		if err != nil {
			if storageNotFound(err) {
				return alismux.NotFoundErr("%s", err.Error())
			}
			return err
		}
		return writeJSON(w, http.StatusOK, result)
	}
}

func (l *evalsLauncher) metricsInfoHandler() alismux.Func {
	return func(w http.ResponseWriter, r *http.Request) error {
		if _, err := l.pathAppName(r); err != nil {
			return err
		}
		if l.registry == nil {
			return alismux.BadRequestErr("%s", missingMetricRegistryMessage)
		}
		info := l.registry.GetRegisteredMetrics()
		sort.Slice(info, func(i, j int) bool {
			return info[i].MetricName < info[j].MetricName
		})
		return writeJSON(w, http.StatusOK, ListMetricsInfoResponse{MetricsInfo: info})
	}
}
