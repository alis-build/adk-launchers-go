package metrics

import "go.alis.build/adk/launchers/evals/evaluation/models"

// trajectoryMetricInfo returns metadata for tool_trajectory_avg_score.
func trajectoryMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:      models.MetricToolTrajectoryAvgScore,
		Description:     "Compares expected vs actual tool call trajectories. Score 1.0 is a perfect match.",
		MetricValueInfo: interval(0, 1),
	}
}

// responseEvalMetricInfo returns metadata for response_evaluation_score (Vertex coherence).
func responseEvalMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:      models.MetricResponseEvaluationScore,
		Description:     "Evaluates how coherent the agent response was. Range [1,5].",
		MetricValueInfo: interval(1, 5),
	}
}

// responseMatchMetricInfo returns metadata for response_match_score (ROUGE-1).
func responseMatchMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:      models.MetricResponseMatchScore,
		Description:     "Compares final response to expected using ROUGE-1. Range [0,1].",
		MetricValueInfo: interval(0, 1),
	}
}

// safetyMetricInfo returns metadata for safety_v1.
func safetyMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:      models.MetricSafetyV1,
		Description:     "Evaluates response safety (harmlessness). Range [0,1].",
		MetricValueInfo: interval(0, 1),
	}
}

// multiTurnTaskSuccessMetricInfo returns metadata for multi_turn_task_success_v1.
func multiTurnTaskSuccessMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:      models.MetricMultiTurnTaskSuccessV1,
		Description:     "Evaluates whether the agent achieved the conversation goal.",
		MetricValueInfo: interval(0, 1),
	}
}

// multiTurnTrajectoryMetricInfo returns metadata for multi_turn_trajectory_quality_v1.
func multiTurnTrajectoryMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:      models.MetricMultiTurnTrajectoryQualityV1,
		Description:     "Evaluates overall multi-turn trajectory quality (reference-free).",
		MetricValueInfo: interval(0, 1),
	}
}

// multiTurnToolUseMetricInfo returns metadata for multi_turn_tool_use_quality_v1.
func multiTurnToolUseMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:      models.MetricMultiTurnToolUseQualityV1,
		Description:     "Evaluates tool use quality across a multi-turn conversation.",
		MetricValueInfo: interval(0, 1),
	}
}

// finalResponseMatchV2MetricInfo returns metadata for final_response_match_v2.
func finalResponseMatchV2MetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:      models.MetricFinalResponseMatchV2,
		Description:     "LLM-judge comparison of final response to expected. Range [0,1].",
		MetricValueInfo: interval(0, 1),
	}
}

// rubricFinalResponseMetricInfo returns metadata for rubric_based_final_response_quality_v1.
func rubricFinalResponseMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:      models.MetricRubricBasedFinalResponseQualityV1,
		Description:     "Rubric-based LLM-judge assessment of final response quality.",
		MetricValueInfo: interval(0, 1),
	}
}

// hallucinationsMetricInfo returns metadata for hallucinations_v1.
func hallucinationsMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:      models.MetricHallucinationsV1,
		Description:     "Detects false or unsupported claims using an LLM judge.",
		MetricValueInfo: interval(0, 1),
	}
}

// rubricToolUseMetricInfo returns metadata for rubric_based_tool_use_quality_v1.
func rubricToolUseMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:      models.MetricRubricBasedToolUseQualityV1,
		Description:     "Rubric-based LLM-judge assessment of tool use.",
		MetricValueInfo: interval(0, 1),
	}
}

// perTurnSimulatorMetricInfo returns metadata for per_turn_user_simulator_quality_v1.
func perTurnSimulatorMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:      models.MetricPerTurnUserSimulatorQualityV1,
		Description:     "Evaluates LLM-backed user simulator turn quality.",
		MetricValueInfo: interval(0, 1),
	}
}

// rubricMultiTurnMetricInfo returns metadata for rubric_based_multi_turn_trajectory_quality_v1.
func rubricMultiTurnMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:      models.MetricRubricBasedMultiTurnTrajectoryQualityV1,
		Description:     "Rubric-based LLM-judge assessment of multi-turn trajectory.",
		MetricValueInfo: interval(0, 1),
	}
}
