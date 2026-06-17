// Package aguimsg converts inbound AG-UI request payloads into genai message parts.
//
// Used by the default [agui.AgentExecutor] when resolving the user turn for a
// /run_sse request: last user message, multimodal [types.InputContent], and
// trailing tool-result messages from client-side tool execution.
//
// This package is internal to [agui]; import agui for the public launcher API.
package aguimsg
