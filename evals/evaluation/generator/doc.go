// Package generator runs agent inference for eval cases.
//
// [Generator] drives the user-simulator loop: for each turn it calls
// [go.alis.build/adk/launchers/internal/adkrun.Runtime] to execute the agent,
// records invocations, and asks a [simulation.UserSimulator] for the next user
// message until the conversation completes or a turn limit is reached.
//
// [RequestInterceptor] can observe or mutate ADK run requests during inference.
// [InferenceOptions.UseLive] enables live (streaming) inference with a timeout.
package generator
