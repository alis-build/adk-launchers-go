package service

import (
	"context"
	"fmt"
	"log"
	"runtime/debug"
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
	parallelism := req.InferenceConfig.Parallelism
	if parallelism <= 0 {
		parallelism = 4
	}

	type item struct {
		idx int
		c   models.EvalCase
	}
	work := make(chan item)
	out := make([]InferenceResult, len(cases))
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
			res := s.runInference(ctx, req.AppName, req.EvalSetID, job.c, req.InferenceConfig)
			out[job.idx] = res
		}
	}

	nWorkers := parallelism
	if nWorkers > len(cases) {
		nWorkers = len(cases)
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
	sendErr := sendJobs(ctx, work, workersDone, len(cases), func(i int) item {
		return item{idx: i, c: cases[i]}
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

// runInference executes agent inference for a single eval case.
func (s *LocalEvalService) runInference(ctx context.Context, appName, evalSetID string, evalCase models.EvalCase, cfg InferenceConfig) InferenceResult {
	sessionID := generator.NewEvalSessionID()
	if s.NewSessionID != nil {
		sessionID = s.NewSessionID()
	}
	result := InferenceResult{
		AppName:    appName,
		EvalSetID:  evalSetID,
		EvalCaseID: evalCase.EvalID,
		SessionID:  sessionID,
	}
	sim, err := s.SimProvider.Provide(evalCase)
	if err != nil {
		result.Status = InferenceStatusFailed
		result.ErrorMessage = err.Error()
		return result
	}
	inv, err := s.Generator.GenerateInferences(ctx, generator.InferenceOptions{
		SessionID:          sessionID,
		SessionInput:       evalCase.SessionInput,
		UserSimulator:      sim,
		UseLive:            cfg.UseLive,
		LiveTimeoutSeconds: cfg.LiveTimeoutSeconds,
	})
	if err != nil {
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
				}
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
		}}
	}
	result, err := evaluator.EvaluateInvocations(ctx, actual, evalCase.Conversation, evalCase.ConversationScenario)
	if err != nil {
		log.Printf("evals: evaluator %q failed for eval %q: %v", metric.MetricName, evalCase.EvalID, err)
		return []models.EvalMetricResult{{
			MetricName: metric.MetricName,
			Threshold:  metric.Threshold,
			EvalStatus: models.EvalStatusNotEvaluated,
		}}
	}

	overall := models.EvalMetricResult{
		MetricName: metric.MetricName,
		Threshold:  metric.Threshold,
		Score:      result.OverallScore,
		EvalStatus: result.OverallEvalStatus,
	}
	if len(result.OverallRubricScores) > 0 {
		overall.Details = &models.EvalMetricResultDetails{RubricScores: result.OverallRubricScores}
	}

	if result.OverallEvalStatus != models.EvalStatusNotEvaluated {
		for i := range perInvocation {
			if i >= len(result.PerInvocationResults) {
				break
			}
			invResult := result.PerInvocationResults[i]
			score := invResult.Score
			status := invResult.EvalStatus
			var details *models.EvalMetricResultDetails
			if len(invResult.RubricScores) > 0 {
				details = &models.EvalMetricResultDetails{RubricScores: invResult.RubricScores}
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
// Passed when at least one metric passed; else NotEvaluated.
func finalEvalStatus(overall []models.EvalMetricResult) models.EvalStatus {
	status := models.EvalStatusNotEvaluated
	for _, m := range overall {
		switch m.EvalStatus {
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
