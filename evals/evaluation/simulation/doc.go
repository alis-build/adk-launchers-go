// Package simulation implements user simulators for multi-turn eval inference.
//
// A [UserSimulator] produces the next user message given ADK session events.
// [StaticUserSimulator] replays a fixed conversation from an eval case.
// [LlmBackedUserSimulator] generates user turns from a [models.ConversationScenario]
// using an LLM content generator.
//
// [UserSimulatorProvider] selects the simulator implementation from eval config
// and is wired into [service.LocalEvalService] by the evals launcher.
//
// A user_simulator_config is decoded by [ParseUserSimulatorConfig] into an
// [LlmBackedUserSimulatorConfig] or an [LlmAudioUserSimulatorConfig] by its
// "type" (missing means llm_backed). Given an audio config, the provider
// wraps every case's simulator, static or scenario, in an
// [AudioUserSimulator]: each text turn is sent to the audio model, and the
// user message carries the speech, resampled to 16 kHz for live input
// ([ToLiveInput]), after the text unless IncludeTextWithAudio is false. A
// scenario's text simulator takes the config's model, turn limit and
// instructions when the factory is an [LlmBackedConfigFactory].
package simulation
