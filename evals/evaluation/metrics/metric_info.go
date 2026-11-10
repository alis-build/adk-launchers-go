package metrics

import "go.alis.build/adk/launchers/evals/evaluation/models"

// trajectoryMetricInfo returns metadata for tool_trajectory_avg_score.
func trajectoryMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:        models.MetricToolTrajectoryAvgScore,
		Description:       "This metric compares two tool call trajectories (expected vs. actual) for the same user interaction. It performs an exact match on the tool name and arguments for each step in the trajectory. A score of 1.0 indicates a perfect match, while 0.0 indicates a mismatch. Higher values are better.",
		MetricValueInfo:   interval(0, 1),
		RequiresThreshold: true,
	}
}

// responseEvalMetricInfo returns metadata for response_evaluation_score (Vertex coherence).
func responseEvalMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:        models.MetricResponseEvaluationScore,
		Description:       "This metric evaluates how coherent agent's response was. Value range of this metric is [1,5], with values closer to 5 more desirable.",
		MetricValueInfo:   interval(1, 5),
		RequiresThreshold: true,
	}
}

// responseMatchMetricInfo returns metadata for response_match_score (ROUGE-1).
func responseMatchMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:        models.MetricResponseMatchScore,
		Description:       "This metric evaluates if the agent's final response matches a golden/expected final response using Rouge_1 metric. Value range for this metric is [0,1], with values closer to 1 more desirable.",
		MetricValueInfo:   interval(0, 1),
		RequiresThreshold: true,
	}
}

// safetyMetricInfo returns metadata for safety_v1.
func safetyMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:        models.MetricSafetyV1,
		Description:       "This metric evaluates the safety (harmlessness) of an Agent's Response. Value range of the metric is [0, 1], with values closer to 1 to be more desirable (safe).",
		MetricValueInfo:   interval(0, 1),
		RequiresThreshold: true,
	}
}

// multiTurnTaskSuccessMetricInfo returns metadata for multi_turn_task_success_v1.
func multiTurnTaskSuccessMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:        models.MetricMultiTurnTaskSuccessV1,
		Description:       "Evaluates if the agent was able to achieve the goal or goals of the conversation. Value range of the metric is [0, 1], with values closer to 1 to be more desirable (safe).",
		MetricValueInfo:   interval(0, 1),
		RequiresThreshold: true,
	}
}

// multiTurnTrajectoryMetricInfo returns metadata for multi_turn_trajectory_quality_v1.
func multiTurnTrajectoryMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:        models.MetricMultiTurnTrajectoryQualityV1,
		Description:       "Evaluates the overall trajectory of the conversation. Note that this metric is different from `Multi-Turn Overall Task Success`, in the sense that task success only concerns itself with the goal of whether the success was achieved or not. How that was achieved is not its concern. This metric on the other hand does care about the path that agent took to achieve the goal. This is a reference free metric. Value range of the metric is [0, 1], with values closer to 1 to be more desirable (safe).",
		MetricValueInfo:   interval(0, 1),
		RequiresThreshold: true,
	}
}

// multiTurnToolUseMetricInfo returns metadata for multi_turn_tool_use_quality_v1.
func multiTurnToolUseMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:        models.MetricMultiTurnToolUseQualityV1,
		Description:       "Evaluates the function calls made during a multi-turn conversation. This is a reference free metric. Value range of the metric is [0, 1], with values closer to 1 to be more desirable (safe).",
		MetricValueInfo:   interval(0, 1),
		RequiresThreshold: true,
	}
}

// finalResponseMatchV2MetricInfo returns metadata for final_response_match_v2.
func finalResponseMatchV2MetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:        models.MetricFinalResponseMatchV2,
		Description:       "This metric evaluates if the agent's final response matches a golden/expected final response using LLM as a judge. Value range for this metric is [0,1], with values closer to 1 more desirable.",
		MetricValueInfo:   interval(0, 1),
		RequiresThreshold: true,
	}
}

// rubricFinalResponseMetricInfo returns metadata for rubric_based_final_response_quality_v1.
func rubricFinalResponseMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:        models.MetricRubricBasedFinalResponseQualityV1,
		Description:       "This metric assess if the agent's final response against a set of rubrics using LLM as a judge. Value range for this metric is [0,1], with values closer to 1 more desirable.",
		MetricValueInfo:   interval(0, 1),
		RequiresThreshold: true,
	}
}

// hallucinationsMetricInfo returns metadata for hallucinations_v1.
func hallucinationsMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:        models.MetricHallucinationsV1,
		Description:       "This metric assesses whether a model response contains any false, contradictory, or unsupported claims using a LLM as judge. Value range for this metric is [0,1], with values closer to 1 more desirable.",
		MetricValueInfo:   interval(0, 1),
		RequiresThreshold: true,
	}
}

// rubricToolUseMetricInfo returns metadata for rubric_based_tool_use_quality_v1.
func rubricToolUseMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:        models.MetricRubricBasedToolUseQualityV1,
		Description:       "This metric assess if the agent's usage of tools against a set of rubrics using LLM as a judge. Value range for this metric is [0,1], with values closer to 1 more desirable.",
		MetricValueInfo:   interval(0, 1),
		RequiresThreshold: true,
	}
}

// perTurnSimulatorMetricInfo returns metadata for per_turn_user_simulator_quality_v1.
func perTurnSimulatorMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:        models.MetricPerTurnUserSimulatorQualityV1,
		Description:       "This metric evaluates if the user messages generated by a user simulator follow the given conversation scenario. It validates each message separately. The resulting metric computes the percentage of user messages that we mark as valid. The value range for this metric is [0,1], with values closer to 1 more desirable. ",
		MetricValueInfo:   interval(0, 1),
		RequiresThreshold: true,
	}
}

// rubricMultiTurnMetricInfo returns metadata for rubric_based_multi_turn_trajectory_quality_v1.
func rubricMultiTurnMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:        models.MetricRubricBasedMultiTurnTrajectoryQualityV1,
		Description:       "This metric evaluates the agent's multi-turn trajectory against a set of user-provided rubrics using an LLM as a judge. Value range for this metric is [0,1], with values closer to 1 more desirable.",
		MetricValueInfo:   interval(0, 1),
		RequiresThreshold: true,
	}
}

// toolCallCountMetricInfo returns metadata for tool_call_count_v1, an informational efficiency metric.
func toolCallCountMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:  models.MetricToolCallCountV1,
		Description: "This metric counts the number of tool (function) calls the agent made per invocation, averaged across the eval case. It is an informational efficiency metric: it reports the value for tracking and does not pass or fail the eval case.",
	}
}

// inferenceCallCountMetricInfo returns metadata for inference_call_count_v1, an informational efficiency metric.
func inferenceCallCountMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:  models.MetricInferenceCallCountV1,
		Description: "This metric counts the number of inference (model) calls the agent made per invocation, averaged across the eval case. It is a proxy for the number of attempts or reasoning steps taken, and read alongside token usage it separates the two ways a turn gets expensive: more calls, or a larger context per call. It is an informational efficiency metric: it reports the value for tracking and does not pass or fail the eval case.",
	}
}

// tokenUsageMetricInfo returns metadata for token_usage_v1, an informational efficiency metric.
func tokenUsageMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:  models.MetricTokenUsageV1,
		Description: "This metric sums the tokens consumed by the model across all model calls in an invocation, averaged across the eval case. The score is the total; every token type is reported alongside it as a nested breakdown -- total, then input (prompt, of which cached, plus tool use) and output (candidates plus reasoning) -- using the same definitions as ADK's telemetry token metrics. It is an informational efficiency metric: it reports the value for tracking and does not pass or fail the eval case.",
	}
}

// invocationDurationMetricInfo returns metadata for invocation_duration_v1, an informational efficiency metric.
func invocationDurationMetricInfo() models.MetricInfo {
	return models.MetricInfo{
		MetricName:  models.MetricInvocationDurationV1,
		Description: "This metric reports the wall-clock seconds an invocation took, averaged across the eval case. The score is the total, and where it went is reported alongside it as a breakdown that partitions the turn: time awaiting a model, time inside tools, and the framework's own time. Tool time is kept separate from the framework's so that a slow external API a tool calls is not read as ADK overhead. The duration is measured while the agent runs; an invocation not produced by this eval run reports no value. Wall-clock time is noisier than the token and call counts, since it moves with model-server load and network, so read it as an indication rather than a regression signal. It is an informational efficiency metric: it reports the value for tracking and does not pass or fail the eval case.",
	}
}
