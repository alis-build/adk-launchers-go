package service

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
	"strings"
	"sync"

	"go.alis.build/adk/launchers/evals/evaluation/generator"
	"go.alis.build/adk/launchers/evals/evaluation/metrics"
	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/simulation"
	"go.alis.build/adk/launchers/evals/evaluation/storage"
	"google.golang.org/adk/v2/session"
)

// LocalEvalService runs inference and metric evaluation locally.
type LocalEvalService struct {
	Generator    *generator.Generator
	Sets         storage.EvalSetsManager
	Results      storage.EvalSetResultsManager
	Registry     *metrics.Registry
	SimProvider  simulation.UserSimulatorProvider
	Sessions     session.Service
	NewSessionID func() string
}

// PerformInference runs agent inference for eval cases in a set.
//
// Per-case failures are returned in InferenceResult with Status=failed; they do
// not abort other cases. The returned error is reserved for operational
// failures (context cancellation, worker panic, or send interruption).
func (s *LocalEvalService) PerformInference(ctx context.Context, req InferenceRequest) ([]InferenceResult, error) {
	if s == nil || s.Generator == nil || s.Sets == nil {
		return nil, fmt.Errorf("local eval service is not configured")
	}
	evalSet, err := s.Sets.GetEvalSet(req.AppName, req.EvalSetID)
	if err != nil {
		return nil, err
	}
	if evalSet == nil {
		return nil, fmt.Errorf("eval set %q not found for app %q", req.EvalSetID, req.AppName)
	}
	cases := filterEvalCases(evalSet.EvalCases, req.EvalCaseIDs)
	if req.InferenceConfig.NumRuns > 1 {
		cases = dropPinnedSessions(cases, req.InferenceConfig.NumRuns)
	}
	// Each case runs NumRuns times through the same worker pool, so
	// Parallelism bounds the repeats too. Results are case-major: case 0's
	// runs 0..n-1, then case 1's, so callers can index them.
	runs := req.InferenceConfig.NumRuns
	if runs <= 0 {
		runs = 1
	}
	parallelism := req.InferenceConfig.Parallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	type item struct {
		idx int
		rep int
		c   models.EvalCase
	}
	jobs := len(cases) * runs
	work := make(chan item)
	out := make([]InferenceResult, jobs)
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	recordErr := func(err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
	}

	worker := func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				recordErr(fmt.Errorf("inference worker panicked: %v\n%s", r, debug.Stack()))
			}
		}()
		for job := range work {
			res := s.runInference(ctx, req.AppName, req.EvalSetID, req.UserID, job.c, job.rep, req.InferenceConfig, req.SessionState)
			out[job.idx] = res
		}
	}

	nWorkers := min(parallelism, jobs)
	if nWorkers == 0 {
		return nil, nil
	}
	workersDone := make(chan struct{})
	for range nWorkers {
		wg.Add(1)
		go worker()
	}
	go func() {
		wg.Wait()
		close(workersDone)
	}()
	sendErr := sendJobs(ctx, work, workersDone, jobs, func(i int) item {
		return item{idx: i, rep: i % runs, c: cases[i/runs]}
	})
	close(work)
	<-workersDone
	if sendErr != nil {
		recordErr(sendErr)
	}
	return out, firstErr
}

// sendJobs pushes n items into work. It aborts on ctx cancellation or when
// all workers have exited (workersDone closed), so a panic in a worker cannot
// deadlock the caller on an unbuffered channel.
func sendJobs[T any](ctx context.Context, work chan<- T, workersDone <-chan struct{}, n int, make func(int) T) error {
	for i := 0; i < n; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-workersDone:
			return fmt.Errorf("all workers exited before jobs completed")
		case work <- make(i):
		}
	}
	return nil
}

// runSessionInput returns a copy of input with the stored app cleared, so the
// generator runs the runtime's own app (the launcher builds one runtime per
// route app), and with userID as the user when it is set. The stored case is
// never modified.
func runSessionInput(input *models.SessionInput, userID string) *models.SessionInput {
	cp := models.SessionInput{}
	if input != nil {
		cp = *input
	}
	cp.AppName = ""
	if userID != "" {
		cp.UserID = userID
	}
	return &cp
}

// dropPinnedSessions returns cases with every pinned session cleared, for a
// run of each case numRuns times: the runs would share one session, so each
// starts a fresh one instead, as in adk-python. The stored cases are not
// modified.
func dropPinnedSessions(cases []models.EvalCase, numRuns int) []models.EvalCase {
	out := make([]models.EvalCase, len(cases))
	for i, c := range cases {
		if c.SessionInput != nil && c.SessionInput.SessionID != "" {
			log.Printf("evals: eval case %q pins session %q, but num_runs is %d, so each run starts a fresh session and the pinned session is not used.", c.EvalID, c.SessionInput.SessionID, numRuns)
			in := *c.SessionInput
			in.SessionID = ""
			c.SessionInput = &in
		}
		out[i] = c
	}
	return out
}

// pinnedSessionExists reports whether input's pinned session exists for the
// user the run uses: the caller when the request names one (runSessionInput
// has already set it), else the case's user or the default eval user. No
// other user's sessions are looked at. Without a session service, or on a
// lookup error, the session counts as absent and the runner creates it.
func (s *LocalEvalService) pinnedSessionExists(ctx context.Context, input *models.SessionInput) bool {
	if s.Sessions == nil {
		return false
	}
	resp, err := s.Sessions.Get(ctx, &session.GetRequest{
		AppName:   s.Generator.Runtime.AppName(),
		UserID:    pinLookupUser(input),
		SessionID: input.SessionID,
	})
	return err == nil && resp != nil && resp.Session != nil
}

// pinLookupUser is the user the generator runs input as: its UserID, or the
// default eval user when that is blank (generator sessionBootstrap).
func pinLookupUser(input *models.SessionInput) string {
	if strings.TrimSpace(input.UserID) == "" {
		return defaultEvalUserID
	}
	return input.UserID
}

// runInference executes agent inference for a single eval case.
//
// The session always runs on the runtime's app, and as userID when it is set,
// whatever the stored case names: a case is shared data, and must not choose
// which agent runs or whose sessions a run writes to.
func (s *LocalEvalService) runInference(ctx context.Context, appName, evalSetID, userID string, evalCase models.EvalCase, rep int, cfg InferenceConfig, runSessionState map[string]any) InferenceResult {
	sessionID := generator.NewEvalSessionID()
	if s.NewSessionID != nil {
		sessionID = s.NewSessionID()
	}
	input := runSessionInput(effectiveSessionInput(runSessionState, evalCase), userID)
	// Blank IDs are treated as unset, as the generator does. Only an eval
	// session can be pinned: a case is shared data, so a pin naming a real
	// session would let its author make another caller's run write into that
	// session and publish its state in the app-wide results.
	if pin := input.SessionID; strings.TrimSpace(pin) != "" && !generator.IsEvalSessionID(pin) {
		log.Printf("evals: eval case %q pins session %q, which is not an eval session (prefix %q), so the run starts a fresh session.", evalCase.EvalID, pin, generator.EvalSessionIDPrefix)
		input.SessionID = ""
	}
	if strings.TrimSpace(input.SessionID) != "" {
		sessionID = input.SessionID
		if s.pinnedSessionExists(ctx, input) {
			// A prepared session keeps its own state, as in adk-python,
			// which applies the case's state only when it creates one.
			input.State = nil
		}
	}
	result := InferenceResult{
		AppName:    appName,
		EvalSetID:  evalSetID,
		EvalCaseID: evalCase.EvalID,
		SessionID:  sessionID,
		// Every run is labelled, a single one with 0, as adk-python does.
		RepetitionIndex: &rep,
		UserID:          evalCaseUserID(&models.EvalCase{SessionInput: input}),
	}
	sim, err := s.SimProvider.Provide(ctx, evalCase)
	if err != nil {
		// The case result records only that it failed, so the reason is
		// logged, as adk-python does.
		log.Printf("evals: inference failed for eval case %q (run %d): %v", evalCase.EvalID, rep, err)
		result.Status = InferenceStatusFailed
		result.ErrorMessage = err.Error()
		return result
	}
	inv, err := s.Generator.GenerateInferences(ctx, generator.InferenceOptions{
		SessionID:          sessionID,
		SessionInput:       input,
		UserSimulator:      sim,
		UseLive:            cfg.UseLive,
		LiveTimeoutSeconds: cfg.LiveTimeoutSeconds,
	})
	if err != nil {
		log.Printf("evals: inference failed for eval case %q (run %d): %v", evalCase.EvalID, rep, err)
		result.Status = InferenceStatusFailed
		result.ErrorMessage = err.Error()
		return result
	}
	result.Inferences = inv
	result.Status = InferenceStatusSuccess
	return result
}

// Evaluate scores inference results with configured metrics.
//
// It returns one EvalCaseResult per inference result, in input order. Per-case
// evaluation problems yield FAILED results rather than omitting entries. The
// returned error indicates operational or persistence failures only.
func (s *LocalEvalService) Evaluate(ctx context.Context, req EvaluateRequest) ([]models.EvalCaseResult, error) {
	if s == nil || s.Sets == nil || s.Registry == nil {
		return nil, fmt.Errorf("local eval service is not configured")
	}
	parallelism := req.EvaluateConfig.Parallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	type item struct {
		idx int
		inf InferenceResult
	}
	type result struct {
		idx int
		res models.EvalCaseResult
	}
	work := make(chan item)
	results := make(chan result, len(req.InferenceResults))
	var wg sync.WaitGroup
	var firstErr error
	var errMu sync.Mutex
	recordErr := func(err error) {
		if err == nil {
			return
		}
		errMu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		errMu.Unlock()
	}

	worker := func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				recordErr(fmt.Errorf("evaluate worker panicked: %v\n%s", r, debug.Stack()))
			}
		}()
		for job := range work {
			res, err := s.evaluateSingle(ctx, job.inf, req.EvaluateConfig)
			if err != nil {
				log.Printf("evals: evaluate case %q: %v", job.inf.EvalCaseID, err)
				res = models.EvalCaseResult{
					EvalSetID:       job.inf.EvalSetID,
					EvalID:          job.inf.EvalCaseID,
					FinalEvalStatus: models.EvalStatusFailed,
					SessionID:       job.inf.SessionID,
					RepetitionIndex: job.inf.RepetitionIndex,
				}
				// A failed case still names its user and session, as in
				// adk-python, so a client can open the session it failed in.
				// The case is loaded best-effort; a missing one leaves the
				// user to the inference result.
				evalCase, _ := s.Sets.GetEvalCase(job.inf.AppName, job.inf.EvalSetID, job.inf.EvalCaseID)
				s.attachSessionMetadata(ctx, job.inf, evalCase, &res)
			}
			results <- result{idx: job.idx, res: res}
		}
	}

	nWorkers := parallelism
	if nWorkers > len(req.InferenceResults) {
		nWorkers = len(req.InferenceResults)
	}
	if nWorkers == 0 {
		return nil, nil
	}
	workersDone := make(chan struct{})
	for range nWorkers {
		wg.Add(1)
		go worker()
	}
	go func() {
		wg.Wait()
		close(workersDone)
	}()

	inferences := req.InferenceResults
	sendErr := sendJobs(ctx, work, workersDone, len(inferences), func(i int) item {
		return item{idx: i, inf: inferences[i]}
	})
	close(work)
	<-workersDone
	close(results)
	if sendErr != nil {
		recordErr(sendErr)
	}

	ordered := make([]models.EvalCaseResult, len(req.InferenceResults))
	for r := range results {
		ordered[r.idx] = r.res
	}

	if s.Results != nil {
		for key, cases := range groupBySetAndApp(inferences, ordered) {
			if _, err := s.Results.SaveEvalSetResult(key.appName, key.setID, cases); err != nil {
				log.Printf("evals: save eval set result app=%q set=%q: %v", key.appName, key.setID, err)
				recordErr(err)
			}
		}
	}
	return ordered, firstErr
}

type setAppKey struct {
	appName string
	setID   string
}

// evaluateSingle scores one inference result against golden data and configured metrics.
func (s *LocalEvalService) evaluateSingle(ctx context.Context, inf InferenceResult, cfg EvaluateConfig) (models.EvalCaseResult, error) {
	evalCase, err := s.Sets.GetEvalCase(inf.AppName, inf.EvalSetID, inf.EvalCaseID)
	if err != nil {
		return models.EvalCaseResult{}, err
	}
	if evalCase == nil {
		return models.EvalCaseResult{}, fmt.Errorf("eval case %q not found", inf.EvalCaseID)
	}
	if inf.Inferences == nil {
		result := models.EvalCaseResult{
			EvalSetID:       inf.EvalSetID,
			EvalID:          inf.EvalCaseID,
			FinalEvalStatus: models.EvalStatusFailed,
			SessionID:       inf.SessionID,
			RepetitionIndex: inf.RepetitionIndex,
		}
		s.attachSessionMetadata(ctx, inf, evalCase, &result)
		return result, nil
	}
	// Static conversation cases require one actual invocation per golden turn.
	if evalCase.ConversationScenario == nil && len(inf.Inferences) != len(evalCase.Conversation) {
		return models.EvalCaseResult{}, fmt.Errorf("inferences length %d != conversation length %d", len(inf.Inferences), len(evalCase.Conversation))
	}

	// Seed per-invocation result shells with actual/expected pairing by index.
	perInvocation := make([]models.EvalMetricResultPerInvocation, len(inf.Inferences))
	for i, actual := range inf.Inferences {
		var expected *models.Invocation
		if i < len(evalCase.Conversation) {
			exp := evalCase.Conversation[i]
			expected = &exp
		}
		perInvocation[i] = models.EvalMetricResultPerInvocation{
			ActualInvocation:   actual,
			ExpectedInvocation: expected,
		}
	}

	actual := append([]models.Invocation(nil), inf.Inferences...)
	// Copy rubrics from golden data onto actual invocations for rubric metrics.
	if err := copyEvalCaseRubricsToActual(*evalCase, actual); err != nil {
		return models.EvalCaseResult{}, err
	}
	if err := copyInvocationRubricsToActual(evalCase.Conversation, actual); err != nil {
		return models.EvalCaseResult{}, err
	}

	var overall []models.EvalMetricResult
	for _, metric := range cfg.EvalMetrics {
		overall = append(overall, s.evaluateMetric(ctx, metric, *evalCase, actual, perInvocation)...)
	}

	result := models.EvalCaseResult{
		EvalSetID:                     inf.EvalSetID,
		EvalID:                        inf.EvalCaseID,
		FinalEvalStatus:               finalEvalStatus(overall),
		OverallEvalMetricResults:      overall,
		EvalMetricResultPerInvocation: perInvocation,
		SessionID:                     inf.SessionID,
		RepetitionIndex:               inf.RepetitionIndex,
	}
	s.attachSessionMetadata(ctx, inf, evalCase, &result)
	return result, nil
}

// groupBySetAndApp buckets case results by (appName, evalSetID). Entries with
// empty EvalSetID are dropped to avoid SaveEvalSetResult validation errors.
func groupBySetAndApp(infs []InferenceResult, cases []models.EvalCaseResult) map[setAppKey][]models.EvalCaseResult {
	out := make(map[setAppKey][]models.EvalCaseResult)
	for i, c := range cases {
		if c.EvalSetID == "" {
			continue
		}
		key := setAppKey{appName: infs[i].AppName, setID: c.EvalSetID}
		out[key] = append(out[key], c)
	}
	return out
}

// evaluateMetric runs one metric evaluator and merges results into perInvocation.
func (s *LocalEvalService) evaluateMetric(
	ctx context.Context,
	metric models.EvalMetric,
	evalCase models.EvalCase,
	actual []models.Invocation,
	perInvocation []models.EvalMetricResultPerInvocation,
) []models.EvalMetricResult {
	evaluator, err := s.Registry.GetEvaluator(metric)
	if err != nil {
		// Log so callers can distinguish "no evaluator registered" from
		// "evaluator returned error"; both map to NotEvaluated in the API
		// response, which is intentional (ADK Python parity).
		log.Printf("evals: no evaluator for metric %q: %v", metric.MetricName, err)
		return []models.EvalMetricResult{{
			MetricName: metric.MetricName,
			Threshold:  metric.Threshold,
			EvalStatus: models.EvalStatusNotEvaluated,
			Details:    &models.EvalMetricResultDetails{},
		}}
	}
	result, err := evaluator.EvaluateInvocations(ctx, actual, evalCase.Conversation, evalCase.ConversationScenario)
	if err != nil {
		log.Printf("evals: evaluator %q failed for eval %q: %v", metric.MetricName, evalCase.EvalID, err)
		return []models.EvalMetricResult{{
			MetricName: metric.MetricName,
			Threshold:  metric.Threshold,
			EvalStatus: models.EvalStatusNotEvaluated,
			Details:    &models.EvalMetricResultDetails{},
		}}
	}

	// Every result carries Details, as adk-python always builds them.
	overall := models.EvalMetricResult{
		MetricName: metric.MetricName,
		Threshold:  metric.Threshold,
		Score:      result.OverallScore,
		EvalStatus: result.OverallEvalStatus,
		Details: &models.EvalMetricResultDetails{
			RubricScores:      result.OverallRubricScores,
			TokenUsageDetails: result.OverallTokenUsageDetails,
			DurationDetails:   result.OverallDurationDetails,
		},
	}

	// An informational metric that did not report every invocation still
	// gets one entry per invocation, with no score and not evaluated, as
	// adk-python keeps the per-invocation list aligned with placeholders.
	if result.OverallEvalStatus == models.EvalStatusInformational && len(result.PerInvocationResults) != len(perInvocation) {
		log.Printf("evals: metric %q returned %d per-invocation results for %d invocations", metric.MetricName, len(result.PerInvocationResults), len(perInvocation))
		for i := range perInvocation {
			perInvocation[i].EvalMetricResults = append(perInvocation[i].EvalMetricResults, models.EvalMetricResult{
				MetricName: metric.MetricName,
				Threshold:  metric.Threshold,
				EvalStatus: models.EvalStatusNotEvaluated,
				Details:    &models.EvalMetricResultDetails{},
			})
		}
		return []models.EvalMetricResult{overall}
	}

	if result.OverallEvalStatus != models.EvalStatusNotEvaluated {
		for i := range perInvocation {
			if i >= len(result.PerInvocationResults) {
				break
			}
			invResult := result.PerInvocationResults[i]
			score := invResult.Score
			status := invResult.EvalStatus
			details := &models.EvalMetricResultDetails{
				RubricScores:      invResult.RubricScores,
				TokenUsageDetails: invResult.TokenUsageDetails,
				DurationDetails:   invResult.DurationDetails,
			}
			perInvocation[i].EvalMetricResults = append(perInvocation[i].EvalMetricResults, models.EvalMetricResult{
				MetricName: metric.MetricName,
				Threshold:  metric.Threshold,
				Score:      score,
				EvalStatus: status,
				Details:    details,
			})
		}
	}
	return []models.EvalMetricResult{overall}
}

// finalEvalStatus aggregates metric statuses: any Failed fails the case; otherwise
// Passed when at least one metric passed; else NotEvaluated. Informational
// metrics never pass or fail a case, as in adk-python.
func finalEvalStatus(overall []models.EvalMetricResult) models.EvalStatus {
	status := models.EvalStatusNotEvaluated
	for _, m := range overall {
		switch m.EvalStatus {
		case models.EvalStatusInformational:
			continue
		case models.EvalStatusPassed:
			status = models.EvalStatusPassed
		case models.EvalStatusFailed:
			return models.EvalStatusFailed
		}
	}
	return status
}

// filterEvalCases returns all cases when ids is empty, otherwise only matching EvalIDs.
func filterEvalCases(cases []models.EvalCase, ids []string) []models.EvalCase {
	if len(ids) == 0 {
		return cases
	}
	allowed := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		allowed[id] = struct{}{}
	}
	var out []models.EvalCase
	for _, c := range cases {
		if _, ok := allowed[c.EvalID]; ok {
			out = append(out, c)
		}
	}
	return out
}
