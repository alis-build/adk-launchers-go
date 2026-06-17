// Package stream maps ADK session events to AG-UI protocol events during a run.
//
// [Processor] drives the outbound event state machine: text and reasoning
// streaming, tool calls, sub-agent steps, state deltas, HITL interrupt outcomes,
// and optional PredictState custom events. Events are written to a [Sink]
// ([WireEmitter] for SSE, [YieldSink] for [agui.AgentExecutor] iterators).
//
// [CallInterceptor.OnEmit] runs in the HTTP handler, not here — sinks emit raw
// AG-UI events; the handler applies wire-level mutation before writing SSE.
//
// This package is internal to [agui]; import agui for the public launcher API
// and [WithGenAIPartConverter] / [WithExecutor] extension points.
package stream
