// Package simulation implements user simulators for multi-turn eval inference.
//
// A [UserSimulator] produces the next user message given ADK session events.
// [StaticUserSimulator] replays a fixed conversation from an eval case.
// [LlmBackedUserSimulator] generates user turns from a [models.ConversationScenario]
// using an LLM content generator.
//
// [UserSimulatorProvider] selects the simulator implementation from eval config
// and is wired into [service.LocalEvalService] by the evals launcher.
package simulation
