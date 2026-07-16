package metrics

import (
	"context"
	"fmt"
	"sync"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

// ErrMetricNotFound indicates the registry has no evaluator for a metric name.
type ErrMetricNotFound struct {
	MetricName string
}

func (e *ErrMetricNotFound) Error() string {
	return fmt.Sprintf("%s not found in registry", e.MetricName)
}

type evaluatorFactory func(models.EvalMetric, *RegistryConfig) (Evaluator, error)

// registryEntry binds metric metadata to its evaluator factory.
type registryEntry struct {
	info    models.MetricInfo
	factory evaluatorFactory
}

// RegistryConfig supplies optional clients for Vertex and LLM-judge metrics.
type RegistryConfig struct {
	JudgeClient  JudgeClient
	VertexClient VertexEvalClient
}

// Registry maps metric names to evaluator factories.
type Registry struct {
	mu      sync.RWMutex
	entries map[string]registryEntry
	config  RegistryConfig
}

// RegisterEvaluator adds or replaces a metric evaluator factory.
func (r *Registry) RegisterEvaluator(info models.MetricInfo, factory evaluatorFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.entries == nil {
		r.entries = make(map[string]registryEntry)
	}
	r.entries[info.MetricName] = registryEntry{info: info, factory: factory}
}

// GetEvaluator returns a new evaluator instance for the metric.
func (r *Registry) GetEvaluator(metric models.EvalMetric) (Evaluator, error) {
	if metric.CustomFunctionPath != nil && *metric.CustomFunctionPath != "" {
		return newCustomMetricEvaluator(metric), nil
	}
	r.mu.RLock()
	entry, ok := r.entries[metric.MetricName]
	cfg := r.config
	r.mu.RUnlock()
	if !ok {
		return nil, &ErrMetricNotFound{MetricName: metric.MetricName}
	}
	return entry.factory(metric, &cfg)
}

// GetRegisteredMetrics returns metadata for all registered metrics.
func (r *Registry) GetRegisteredMetrics() []models.MetricInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]models.MetricInfo, 0, len(r.entries))
	for _, entry := range r.entries {
		out = append(out, entry.info)
	}
	return out
}

// SetConfig updates injectable clients used by evaluators.
func (r *Registry) SetConfig(cfg RegistryConfig) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.config = cfg
}

// interval builds a MetricValueInfo with closed [min, max] bounds.
func interval(min, max float64) models.MetricValueInfo {
	return models.MetricValueInfo{
		Interval: models.MetricValueInterval{MinValue: min, MaxValue: max},
	}
}

// DefaultRegistry is the prebuilt metric registry matching adk-python.
var DefaultRegistry = NewDefaultRegistry()

// NewDefaultRegistry builds a registry with all standard metrics registered.
func NewDefaultRegistry() *Registry {
	r := &Registry{entries: make(map[string]registryEntry)}
	registerAllPrebuiltMetrics(r)
	return r
}

// registerAllPrebuiltMetrics registers the standard adk-python metric set.
func registerAllPrebuiltMetrics(r *Registry) {
	r.RegisterEvaluator(trajectoryMetricInfo(), func(m models.EvalMetric, _ *RegistryConfig) (Evaluator, error) {
		return newTrajectoryEvaluator(m)
	})
	r.RegisterEvaluator(responseEvalMetricInfo(), func(m models.EvalMetric, cfg *RegistryConfig) (Evaluator, error) {
		return newVertexSingleTurnEvaluator(m, vertexMetricCoherence, true, cfg), nil
	})
	r.RegisterEvaluator(responseMatchMetricInfo(), func(m models.EvalMetric, _ *RegistryConfig) (Evaluator, error) {
		return newResponseMatchEvaluator(m), nil
	})
	r.RegisterEvaluator(safetyMetricInfo(), func(m models.EvalMetric, cfg *RegistryConfig) (Evaluator, error) {
		return newVertexSingleTurnEvaluator(m, vertexMetricSafety, true, cfg), nil
	})
	r.RegisterEvaluator(multiTurnTaskSuccessMetricInfo(), func(m models.EvalMetric, cfg *RegistryConfig) (Evaluator, error) {
		return newVertexMultiTurnEvaluator(m, vertexMetricMultiTurnTaskSuccess, cfg), nil
	})
	r.RegisterEvaluator(multiTurnTrajectoryMetricInfo(), func(m models.EvalMetric, cfg *RegistryConfig) (Evaluator, error) {
		return newVertexMultiTurnEvaluator(m, vertexMetricMultiTurnTrajectoryQuality, cfg), nil
	})
	r.RegisterEvaluator(multiTurnToolUseMetricInfo(), func(m models.EvalMetric, cfg *RegistryConfig) (Evaluator, error) {
		return newVertexMultiTurnEvaluator(m, vertexMetricMultiTurnToolUseQuality, cfg), nil
	})
	r.RegisterEvaluator(finalResponseMatchV2MetricInfo(), func(m models.EvalMetric, cfg *RegistryConfig) (Evaluator, error) {
		return newFinalResponseMatchV2Evaluator(m, cfg), nil
	})
	r.RegisterEvaluator(rubricFinalResponseMetricInfo(), func(m models.EvalMetric, cfg *RegistryConfig) (Evaluator, error) {
		return newRubricBasedEvaluator(m, rubricKindFinalResponse, cfg), nil
	})
	r.RegisterEvaluator(hallucinationsMetricInfo(), func(m models.EvalMetric, cfg *RegistryConfig) (Evaluator, error) {
		return newHallucinationsEvaluator(m, cfg), nil
	})
	r.RegisterEvaluator(rubricToolUseMetricInfo(), func(m models.EvalMetric, cfg *RegistryConfig) (Evaluator, error) {
		return newRubricBasedEvaluator(m, rubricKindToolUse, cfg), nil
	})
	r.RegisterEvaluator(perTurnSimulatorMetricInfo(), func(m models.EvalMetric, cfg *RegistryConfig) (Evaluator, error) {
		return newPerTurnSimulatorEvaluator(m, cfg), nil
	})
	r.RegisterEvaluator(rubricMultiTurnMetricInfo(), func(m models.EvalMetric, cfg *RegistryConfig) (Evaluator, error) {
		return newRubricBasedEvaluator(m, rubricKindMultiTurnTrajectory, cfg), nil
	})
}

// customMetricEvaluator is a placeholder for user-defined metrics referenced
// by CustomFunctionPath; evaluation returns NotEvaluated until registered.
type customMetricEvaluator struct {
	metric models.EvalMetric
}

// newCustomMetricEvaluator returns an evaluator stub for custom metric paths.
func newCustomMetricEvaluator(metric models.EvalMetric) Evaluator {
	return &customMetricEvaluator{metric: metric}
}

func (e *customMetricEvaluator) EvaluateInvocations(context.Context, []models.Invocation, []models.Invocation, *models.ConversationScenario) (EvaluationResult, error) {
	return EvaluationResult{OverallEvalStatus: models.EvalStatusNotEvaluated}, nil
}
