package stream

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
	"github.com/google/jsonschema-go/jsonschema"
	"go.alis.build/adk/launchers/agui/internal/interrupt"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"
)

// bufPool reuses byte buffers for JSON serialization on the SSE event-emission
// hot path (tool args, function responses, interrupt payloads).
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// Sink receives AG-UI protocol events during a run.
type Sink interface {
	Emit(events.Event)
	Err() error
}

// PartConverter converts a genai.Part from an ADK session event into AG-UI events.
type PartConverter func(ctx context.Context, adkEvent *session.Event, part *genai.Part) ([]events.Event, error)

// PredictStateMapping declares how a tool call argument maps to optimistic state preview.
type PredictStateMapping struct {
	StateKey     string `json:"state_key"`
	Tool         string `json:"tool"`
	ToolArgument string `json:"tool_argument"`
}

// Processor maps ADK session events to AG-UI protocol events.
type Processor struct {
	DefaultPartConverter   PartConverter
	LoadSessionForSnapshot func(ctx context.Context, appName, userID, sessionID string) (session.Session, bool, error)
	BuildMessagesSnapshot  func(ctx context.Context, sess session.Session) ([]types.Message, error)
	IsInternalStateKey     func(key string) bool
	BuildStateSnapshot     func(sess session.Session, reqState map[string]any) map[string]any

	// InterruptReasonClassifier optionally overrides the AG-UI reason chosen for
	// a workflow input request. Nil falls through to the schema-shape rule.
	InterruptReasonClassifier interrupt.ReasonClassifier

	// GraphAttributionDisabled suppresses workflow graph attribution: node step
	// events, node outputs in state, and node metadata on interrupts. The
	// agent's own output is unaffected.
	GraphAttributionDisabled bool
}

// nodeProvenance reads graph attribution for an event, honouring the launcher's
// opt-out. Every attribution site goes through here so a single flag turns all
// of them off together and none can drift out of step.
func (p *Processor) nodeProvenance(ev *session.Event) (NodeProvenance, bool) {
	if p.GraphAttributionDisabled {
		return NodeProvenance{}, false
	}
	return NodeProvenanceFrom(ev)
}

// eventSink is the legacy internal name used within this package.
type eventSink = Sink

// WireEmitter wraps the SSE writer and captures the first write error.
type WireEmitter struct {
	ctx    context.Context
	w      http.ResponseWriter
	writer *sse.SSEWriter
	errVal error
}

func NewWireEmitter(ctx context.Context, w http.ResponseWriter, writer *sse.SSEWriter) *WireEmitter {
	return &WireEmitter{ctx: ctx, w: w, writer: writer}
}

func (e *WireEmitter) Emit(event events.Event) {
	if e.errVal != nil {
		return
	}
	e.errVal = e.writer.WriteEvent(e.ctx, e.w, event)
}

func (e *WireEmitter) Err() error {
	return e.errVal
}

func (e *WireEmitter) SetErr(err error) {
	if e.errVal == nil {
		e.errVal = err
	}
}

// YieldSink forwards events to an iter.Seq2 yield function.
type YieldSink struct {
	yield           func(events.Event, error) bool
	consumerStopped bool
	errVal          error
}

func NewYieldSink(yield func(events.Event, error) bool) *YieldSink {
	return &YieldSink{yield: yield}
}

func (s *YieldSink) Emit(event events.Event) {
	if s.consumerStopped || s.errVal != nil {
		return
	}
	if !s.yield(event, nil) {
		s.consumerStopped = true
	}
}

func (s *YieldSink) Err() error {
	return s.errVal
}

func (s *YieldSink) Stopped() bool {
	return s.consumerStopped
}

// SetErr records a terminal error and, if the consumer is still active, yields it as
// an error event and marks the sink as stopped. This side effect is intentional: it
// ensures the iterator consumer sees the error even if no further Emit calls happen.
func (s *YieldSink) SetErr(err error) {
	s.errVal = err
	if err != nil && !s.consumerStopped {
		s.yield(nil, err)
		s.consumerStopped = true
	}
}

// State tracks the AG-UI event state machine across a run.
type State struct {
	RunID                     string
	ThreadID                  string
	UserID                    string
	RunCtx                    context.Context
	ReqState                  map[string]any
	CurrentTextMessageID      string
	CurrentReasoningPhaseID   string
	CurrentReasoningMessageID string
	LastTextMessageID         string
	CurrentStepName           string
	RootAppName               string
	StreamedReasoning         string          // accumulated partial reasoning text of the current streamed message; classifies non-partial thought events as repeat vs independent
	StreamedText              strings.Builder // partial text deltas streamed for the current message; classifies non-partial text events as repeat vs independent
	RunFinalized              bool
	EmittedInterrupts         []types.Interrupt
	EmittedToolCallArgsJSON   map[string]string
	PredictStateMappings      map[string][]PredictStateMapping
	EmittedPredictStateTools  map[string]bool
	// NodeOutputs accumulates workflow node results for the run, keyed by node
	// path (or agent name for pathless nodes). Surfaced to clients under the
	// reserved _adk state key.
	NodeOutputs map[string]any
}

// emitToolCallLifecycle emits TOOL_CALL_START/ARGS/END for a tool proposal.
//
// Duplicate streaming events with the same toolCallID are skipped only when args
// match (canonical JSON). Different args for the same ID emit another lifecycle so
// clients see the latest proposal. Empty toolCallID is never deduplicated (avoids
// unrelated calls sharing one bucket). Args are marshaled before any SSE write;
// the dedup map is updated only after all three emits succeed (no e.err).
func emitToolCallLifecycle(sink eventSink, state *State, toolCallID, toolCallName string, args map[string]any, startOpts []events.ToolCallStartOption) error {
	if strings.TrimSpace(toolCallID) == "" {
		return fmt.Errorf("function call missing toolCallId")
	}

	argsJSON, err := MarshalPooled(args)
	if err != nil {
		return fmt.Errorf("failed to marshal function call args: %w", err)
	}

	if state.EmittedToolCallArgsJSON != nil {
		if prev, ok := state.EmittedToolCallArgsJSON[toolCallID]; ok && prev == argsJSON {
			return nil
		}
	}

	sink.Emit(events.NewToolCallStartEvent(toolCallID, toolCallName, startOpts...))
	sink.Emit(events.NewToolCallArgsEvent(toolCallID, argsJSON))
	sink.Emit(events.NewToolCallEndEvent(toolCallID))

	if sink.Err() != nil {
		return sink.Err()
	}

	if state.EmittedToolCallArgsJSON == nil {
		state.EmittedToolCallArgsJSON = make(map[string]string)
	}
	state.EmittedToolCallArgsJSON[toolCallID] = argsJSON
	return nil
}

// ProcessEvent maps a single ADK session.Event to the corresponding AG-UI SSE events.
// It manages three state machines:
//   - Text streaming: TextMessageStart -> TextMessageContent* -> TextMessageEnd
//   - Reasoning: ReasoningStart -> ReasoningMessageStart -> ReasoningMessageContent* -> ReasoningMessageEnd -> ReasoningEnd
//   - Sub-agent steps: StepStarted -> StepFinished (triggered by Author changes)
//
// Tool calls are emitted atomically (Start+Args+End); duplicate partials with the
// same toolCallID and args are skipped; same ID with different args re-emits.
//
// Returns (done, err). When done is true the run has been finalized (e.g. an
// interrupt was emitted) and the caller should stop processing events.
func (p *Processor) ProcessEvent(sink eventSink, ev *session.Event, state *State, partConverter PartConverter) (bool, error) {
	// Emit step events when the active producer changes.
	//
	// A workflow event is bracketed by its graph node, named by node path, or by
	// the node's agent name for top-level static nodes which carry no path.
	// Everything else keeps the author-change behaviour: the root agent gets no
	// step, so its author normalizes to "" and consecutive root partials do not
	// repeatedly trip the change check.
	//
	// Close any open text message first so the next partial opens a fresh
	// TEXT_MESSAGE_START with the new author's name (do not rely on ADK turn boundaries).
	stepName := ev.Author
	stepMayChange := ev.Author != ""
	if prov, ok := p.nodeProvenance(ev); ok {
		stepName = prov.StepName(ev.Author)
		// A node is a real graph activation even when its agent shares the
		// root's name, so it is not normalized away; hiding it would drop a
		// node from the client's view of the graph.
		stepMayChange = stepName != ""
	} else if stepName == state.RootAppName {
		stepName = ""
	}
	if stepMayChange && stepName != state.CurrentStepName {
		closeTextMessage(sink, state)
		closeReasoningMessage(sink, state)
		// A different producer follows its own streaming convention; its text
		// must not be deduped against the previous author's streamed content.
		state.StreamedText.Reset()
		state.StreamedReasoning = ""
		if state.CurrentStepName != "" {
			sink.Emit(events.NewStepFinishedEvent(state.CurrentStepName))
		}
		if stepName != "" {
			sink.Emit(events.NewStepStartedEvent(stepName))
		}
		state.CurrentStepName = stepName
	}

	if prov, ok := p.nodeProvenance(ev); ok {
		recordNodeOutput(sink, state, ev, prov)
	}

	if ev.Content != nil {
		// Interrupts are collected across every part and emitted together: the
		// AG-UI protocol allows one terminal event per run, so returning on the
		// first one would leave any later proposal unanswerable by the client.
		var pendingInterrupts []types.Interrupt

		for _, part := range ev.Content.Parts {
			if sink.Err() != nil {
				return false, sink.Err()
			}
			if part == nil {
				continue
			}

			// Let the consumer's part converter handle the part first.
			// A non-nil return (even empty) means "handled, skip default".
			if partConverter == nil {
				partConverter = p.DefaultPartConverter
			}
			if partConverter != nil {
				customEvents, err := partConverter(state.RunCtx, ev, part)
				if err != nil {
					return false, fmt.Errorf("GenAIPartConverter: %w", err)
				}
				if customEvents != nil {
					for _, ce := range customEvents {
						sink.Emit(ce)
					}
					continue
				}
			}

			// Reasoning / thought parts: map to REASONING_* event lifecycle.
			// ReasoningStart/End bracket the phase; ReasoningMessageStart/Content/End
			// bracket individual messages within it. Per the AG-UI spec, these use
			// separate IDs.
			//
			// ADK streaming partials carry accumulated thought text, not deltas:
			// emit only the unseen suffix. A non-partial event that repeats the
			// streamed accumulation (ADK's trailing final) is skipped; while the
			// streamed message is still open it may also extend it, in which case
			// only the unseen suffix is emitted. Any other non-partial thought
			// text comes from a producer that never streamed (e.g. a remote A2A
			// sub-agent aggregating server-side) and is emitted whole. Once the
			// message is closed, a prefix match no longer implies continuation,
			// so the stale accumulation is dropped instead of slicing new text.
			if part.Thought && part.Text != "" {
				text := part.Text
				if ev.Partial {
					if state.CurrentReasoningMessageID == "" {
						// A new streamed message restarts accumulation.
						state.StreamedReasoning = ""
					}
					if len(text) <= len(state.StreamedReasoning) {
						continue
					}
					text = text[len(state.StreamedReasoning):]
					state.StreamedReasoning = part.Text
				} else if streamed := state.StreamedReasoning; streamed != "" {
					switch {
					case text == streamed:
						// Trailing final repeating the streamed thought exactly.
						continue
					case state.CurrentReasoningMessageID != "" && strings.HasPrefix(text, streamed):
						// Mid-message final extending the stream: emit the tail.
						// An independent chunk sharing the streamed prefix is
						// indistinguishable from this and treated as extension.
						text = text[len(streamed):]
						state.StreamedReasoning = part.Text
					case state.CurrentReasoningMessageID == "":
						// New thought after the streamed message closed.
						state.StreamedReasoning = ""
					}
				}

				closeTextMessage(sink, state)

				if state.CurrentReasoningPhaseID == "" {
					state.CurrentReasoningPhaseID = events.GenerateMessageID()
					sink.Emit(events.NewReasoningStartEvent(state.CurrentReasoningPhaseID))
				}
				if state.CurrentReasoningMessageID == "" {
					state.CurrentReasoningMessageID = events.GenerateMessageID()
					sink.Emit(events.NewReasoningMessageStartEvent(state.CurrentReasoningMessageID, "reasoning"))
				}
				sink.Emit(events.NewReasoningMessageContentEvent(state.CurrentReasoningMessageID, text))
				continue
			}

			// Text parts (non-thought): map to TEXT_MESSAGE_* event lifecycle.
			if part.Text != "" && !part.Thought {
				// Close any open reasoning message before emitting text.
				closeReasoningMessage(sink, state)

				// ADK streaming emits partial events with delta text, then a
				// trailing non-partial event repeating the accumulated text —
				// skipped as an exact repeat, or deduped down to its unseen tail
				// while the streamed message is still open (streaming stopped
				// short). Any other non-partial text is an independent chunk
				// from a producer that never streamed (a remote A2A sub-agent
				// aggregating server-side, or a root agent with streaming
				// disabled) and is emitted whole, never sliced. StreamedText —
				// populated only by partial deltas and their tails — tells the
				// two apart by content. Once the message is closed, a prefix
				// match no longer implies continuation, so the stale
				// accumulation is dropped instead of slicing a new message.
				text := part.Text
				if ev.Partial {
					if state.CurrentTextMessageID == "" {
						// A new streamed message restarts accumulation.
						state.StreamedText.Reset()
					}
					state.StreamedText.WriteString(text)
				} else if streamed := state.StreamedText.String(); streamed != "" {
					switch {
					case text == streamed:
						// Trailing final repeating the streamed text exactly.
						continue
					case state.CurrentTextMessageID != "" && strings.HasPrefix(text, streamed):
						// Mid-message final extending the stream: emit the tail.
						// An independent chunk sharing the streamed prefix is
						// indistinguishable from this and treated as extension.
						text = text[len(streamed):]
						state.StreamedText.WriteString(text)
					case state.CurrentTextMessageID == "":
						// New message after the streamed one closed.
						state.StreamedText.Reset()
					}
				}

				if state.CurrentTextMessageID == "" {
					state.CurrentTextMessageID = events.GenerateMessageID()
					// Blank / whitespace-only authors are trimmed so the wire
					// JSON omits "name" via omitempty on the upstream field.
					sink.Emit(events.NewTextMessageStartEvent(
						state.CurrentTextMessageID,
						events.WithRole("assistant"),
						events.WithName(strings.TrimSpace(ev.Author)),
					))
				}
				sink.Emit(events.NewTextMessageContentEvent(state.CurrentTextMessageID, text))
				continue
			}

			// Function call handling. Two cases:
			//
			// 1. An interrupt-producing call (see interruptCallHandlers): ADK's
			//    HITL primitives. Convert to an AG-UI interrupt and finalize
			//    the run.
			//
			// 2. All other function calls: emit ToolCallStart -> ToolCallArgs ->
			//    ToolCallEnd atomically. ADK provides complete args in a single
			//    FunctionCall (not streamed incrementally).
			if part.FunctionCall != nil {
				closeTextMessage(sink, state)
				closeReasoningMessage(sink, state)

				if handle, ok := interruptCallHandlers[part.FunctionCall.Name]; ok {
					// Close open text/reasoning before the first proposal's
					// tool lifecycle, matching the single-interrupt ordering.
					if len(pendingInterrupts) == 0 {
						finalizeLifecycle(sink, state)
					}
					intr, err := handle(p, sink, state, part.FunctionCall, ev)
					if err != nil {
						return false, err
					}
					if prov, ok := p.nodeProvenance(ev); ok {
						annotateNodeProvenance(&intr, prov)
					}
					pendingInterrupts = append(pendingInterrupts, intr)
					continue
				}

				// Emit PredictState custom event before tool call when configured.
				emitPredictStateIfConfigured(sink, state, part.FunctionCall.Name)

				var opts []events.ToolCallStartOption
				if state.LastTextMessageID != "" {
					opts = append(opts, events.WithParentMessageID(state.LastTextMessageID))
				}
				if err := emitToolCallLifecycle(sink, state, part.FunctionCall.ID, part.FunctionCall.Name, part.FunctionCall.Args, opts); err != nil {
					return false, err
				}
				continue
			}

			// Function response: emit ToolCallResult with the serialized response.
			// Each result gets its own unique messageID (distinct from toolCallID).
			// Skip "pending" responses from client proxy tools — these are
			// internal LRO signals, not real results for the SSE stream.
			if part.FunctionResponse != nil {
				if IsPendingProxyResponse(part.FunctionResponse.Response) {
					continue
				}
				respJSON, err := MarshalPooled(part.FunctionResponse.Response)
				if err != nil {
					return false, fmt.Errorf("failed to marshal function response: %w", err)
				}
				resultMsgID := events.GenerateMessageID()
				sink.Emit(events.NewToolCallResultEvent(resultMsgID, part.FunctionResponse.ID, respJSON))
				continue
			}
		}

		if len(pendingInterrupts) > 0 {
			if err := p.finishWithInterrupts(sink, state, pendingInterrupts); err != nil {
				return false, err
			}
			return true, nil
		}
	}

	// Emit state delta when the agent modifies session state.
	// ADK provides a flat map of changed keys; we convert each entry to a
	// JSON Patch "add" operation (RFC 6902). "add" is used instead of "replace"
	// because it works for both creating new keys and updating existing ones,
	// whereas "replace" fails if the path doesn't exist on the client.
	if len(ev.Actions.StateDelta) > 0 {
		ops := make([]events.JSONPatchOperation, 0, len(ev.Actions.StateDelta))
		for key, val := range ev.Actions.StateDelta {
			if p.IsInternalStateKey != nil && p.IsInternalStateKey(key) {
				continue
			}
			ops = append(ops, events.JSONPatchOperation{
				Op:    "add",
				Path:  "/" + EscapeJSONPointer(key),
				Value: val,
			})
		}
		if len(ops) > 0 {
			sink.Emit(events.NewStateDeltaEvent(ops))
		}
	}

	// On turn completion, close all open lifecycle events.
	if ev.TurnComplete {
		finalizeLifecycle(sink, state)
	}

	return false, sink.Err()
}

// finalizeLifecycle closes any open text messages, reasoning phases, and
// sub-agent steps. Must be called before any run-terminal event (RunFinished,
// RunError) to satisfy the AG-UI protocol requirement that all steps are closed
// before the run ends.
func finalizeLifecycle(sink eventSink, state *State) {
	closeTextMessage(sink, state)
	closeReasoningMessage(sink, state)
	if state.CurrentStepName != "" {
		sink.Emit(events.NewStepFinishedEvent(state.CurrentStepName))
		state.CurrentStepName = ""
	}
}

// closeTextMessage emits a TextMessageEndEvent for the currently open text message
// and records it as lastTextMessageID for use as parentMessageID on subsequent tool calls.
//
// StreamedText deliberately survives the close: ADK's trailing non-partial final
// can arrive after a tool call or TurnComplete already closed the message, and
// must still be recognized as an exact repeat of the streamed text.
func closeTextMessage(sink eventSink, state *State) {
	if state.CurrentTextMessageID == "" {
		return
	}
	sink.Emit(events.NewTextMessageEndEvent(state.CurrentTextMessageID))
	state.LastTextMessageID = state.CurrentTextMessageID
	state.CurrentTextMessageID = ""
}

// closeReasoningMessage emits ReasoningMessageEnd and ReasoningEnd events
// to close the currently open reasoning message and phase.
//
// StreamedReasoning deliberately survives the close for the same reason as
// StreamedText in [closeTextMessage]: the trailing final's exact repeat must
// still be deduped after the message has closed.
func closeReasoningMessage(sink eventSink, state *State) {
	if state.CurrentReasoningMessageID != "" {
		sink.Emit(events.NewReasoningMessageEndEvent(state.CurrentReasoningMessageID))
		state.CurrentReasoningMessageID = ""
	}
	if state.CurrentReasoningPhaseID != "" {
		sink.Emit(events.NewReasoningEndEvent(state.CurrentReasoningPhaseID))
		state.CurrentReasoningPhaseID = ""
	}
}

// interruptCallHandler emits any per-interrupt lifecycle events for one ADK
// interrupt-producing FunctionCall and returns the AG-UI interrupt it maps to.
//
// Handlers deliberately do not emit the terminal event. The AG-UI protocol
// allows exactly one RunFinished per run, so an event carrying several
// interrupt calls has to collect them all and finalize once.
type interruptCallHandler func(p *Processor, sink eventSink, state *State, fc *genai.FunctionCall, ev *session.Event) (types.Interrupt, error)

// interruptCallHandlers maps ADK's synthetic HITL FunctionCall names to the
// handler that turns them into AG-UI interrupts. A name absent from this table
// is an ordinary tool call.
//
// Membership doubles as the "is this call interrupt-producing?" predicate, which
// is what lets ProcessEvent recognise an interrupt without knowing how to emit
// one.
//
// See https://docs.ag-ui.com/concepts/interrupts#reason-taxonomy
var interruptCallHandlers = map[string]interruptCallHandler{
	toolconfirmation.FunctionCallName:      buildToolCallInterrupt,
	workflow.WorkflowInputFunctionCallName: buildInputRequestInterrupt,
}

// buildToolCallInterrupt converts an adk_request_confirmation FunctionCall into
// an AG-UI tool-bound interrupt.
//
// Flow (see https://docs.ag-ui.com/concepts/interrupts#tool-bound-interrupts):
//  1. Emit ToolCallStart/Args/End for the original tool (agent proposal).
//  2. Return an Interrupt whose id is fc.ID, so clients resume with that id as
//     interruptId.
//
// The terminal RunFinished is emitted by [Processor.finishWithInterrupts] once
// every interrupt in the event has been built.
//
// The resumed run should not re-emit tool call lifecycle events; ADK continues
// after the client sends a FunctionResponse via [resumeEntriesToConfirmationContent].
func buildToolCallInterrupt(p *Processor, sink eventSink, state *State, fc *genai.FunctionCall, ev *session.Event) (types.Interrupt, error) {
	invocationID := ev.InvocationID
	originalCall, err := toolconfirmation.OriginalCallFrom(fc)
	if err != nil {
		return types.Interrupt{}, fmt.Errorf("failed to extract original call from confirmation: %w", err)
	}

	tc, tcErr := ExtractToolConfirmation(fc)
	hintMessage := tc.Hint

	// Emit ToolCall events for the original tool (the agent's proposal) when not
	// already emitted from earlier streaming events (duplicate partial FCs).
	var startOpts []events.ToolCallStartOption
	if state.LastTextMessageID != "" {
		startOpts = append(startOpts, events.WithParentMessageID(state.LastTextMessageID))
	}
	if err := emitToolCallLifecycle(sink, state, originalCall.ID, originalCall.Name, originalCall.Args, startOpts); err != nil {
		return types.Interrupt{}, err
	}

	adkMeta := map[string]any{
		"confirmationCallId":   fc.ID,
		"confirmationCallName": toolconfirmation.FunctionCallName,
	}
	if invocationID != "" {
		adkMeta["invocationId"] = invocationID
	}
	if tc.Payload != nil {
		adkMeta["confirmationPayload"] = tc.Payload
	}
	interruptMeta := map[string]any{
		"adk": adkMeta,
	}
	if hintMessage != "" {
		interruptMeta["hitl"] = map[string]any{"summary": hintMessage}
	}
	if tcErr != nil {
		log.Printf("agui: emitInterrupt: extractToolConfirmation: %v", tcErr)
	}

	// intr.ID doubles as the ADK confirmation call id for resume correlation.
	// Named intr, not interrupt: the latter shadows the interrupt package, whose
	// constants and helpers are used just below.
	intr := types.Interrupt{
		ID:             fc.ID,
		Reason:         interrupt.ReasonToolCall,
		Message:        hintMessage,
		ToolCallID:     originalCall.ID,
		ResponseSchema: interrupt.ToolConfirmationResponseSchema(),
		Metadata:       interruptMeta,
	}
	return intr, nil
}

// buildInputRequestInterrupt converts an adk_request_input FunctionCall into an
// AG-UI non-tool interrupt.
//
// No tool lifecycle is emitted and ToolCallID is left empty: per the AG-UI
// spec a non-tool reason has no bound tool call, and there is no proposed tool
// here — the agent is asking the human a question directly.
func buildInputRequestInterrupt(p *Processor, _ eventSink, _ *State, fc *genai.FunctionCall, ev *session.Event) (types.Interrupt, error) {
	req := inputRequestFrom(ev, fc)
	responseSchema := interrupt.SchemaToMap(req.ResponseSchema)

	// callName is what resume dispatches on. Reason cannot serve that role: a
	// host classifier may return any custom string, which says nothing about
	// which ADK call has to be answered.
	adkMeta := map[string]any{
		"callName": workflow.WorkflowInputFunctionCallName,
	}
	if ev.InvocationID != "" {
		adkMeta["invocationId"] = ev.InvocationID
	}
	if req.Payload != nil {
		// Clients render this alongside the prompt: the document to approve,
		// the parameters being proposed.
		adkMeta["requestPayload"] = req.Payload
	}

	return types.Interrupt{
		ID:             req.InterruptID,
		Reason:         interrupt.ClassifyReason(req, responseSchema, p.InterruptReasonClassifier),
		Message:        req.Message,
		ResponseSchema: responseSchema,
		Metadata:       map[string]any{"adk": adkMeta},
	}, nil
}

// inputRequestFrom reads the workflow input request behind an adk_request_input
// call, preferring the typed [session.Event.RequestedInput] over the mirrored
// FunctionCall args.
//
// Both carry the same request. The typed field is written by the emitting node,
// while the args are the copy that survives a session round-trip through
// clients that do not model RequestedInput, so either may be the only one
// present and the typed field wins when they disagree.
//
// An event carries at most one typed request but may carry several
// adk_request_input calls, so the typed field is authoritative only for the call
// it names. Applying it to every call would give each interrupt the same id,
// which collapses in resume validation's id map and strands every request but
// the first.
func inputRequestFrom(ev *session.Event, fc *genai.FunctionCall) session.RequestInput {
	if ev.RequestedInput != nil && ev.RequestedInput.InterruptID == fc.ID {
		return *ev.RequestedInput
	}

	req := session.RequestInput{InterruptID: fc.ID}
	if fc.Args == nil {
		return req
	}
	if v, ok := fc.Args["interruptId"].(string); ok && v != "" {
		req.InterruptID = v
	}
	if v, ok := fc.Args["message"].(string); ok {
		req.Message = v
	}
	req.Payload = fc.Args["payload"]
	req.ResponseSchema = schemaFromArg(fc.Args["responseSchema"])
	return req
}

// schemaFromArg reads a response schema out of FunctionCall args, which hold a
// typed *jsonschema.Schema when freshly emitted and a decoded JSON object once
// the event has round-tripped through session state.
//
// The decoded form is re-typed rather than passed straight through so that a
// host classifier always receives a RequestInput with its schema populated,
// whichever path the event arrived by.
func schemaFromArg(v any) *jsonschema.Schema {
	switch arg := v.(type) {
	case *jsonschema.Schema:
		return arg
	case jsonschema.Schema:
		return &arg
	case map[string]any:
		data, err := json.Marshal(arg)
		if err == nil {
			var schema jsonschema.Schema
			if err = json.Unmarshal(data, &schema); err == nil {
				return &schema
			}
		}
		// A malformed schema costs the client its form hints. Dropping it beats
		// failing the run, which would cost the user their turn.
		log.Printf("agui: input request responseSchema ignored: %v", err)
		return nil
	default:
		return nil
	}
}

// finishWithInterrupts emits the single terminal RunFinished carrying every
// interrupt collected from an event, and marks the run finalized.
//
// Snapshots go out first, per the AG-UI spec, so clients can resume from
// persisted state and message history. They are emitted once for the event
// rather than once per interrupt: they describe the session, not the individual
// pause. See https://docs.ag-ui.com/concepts/interrupts
func (p *Processor) finishWithInterrupts(sink eventSink, state *State, intrs []types.Interrupt) error {
	// Close anything still open (a text or reasoning message opened by a part
	// after the last interrupt call) before the terminal event.
	finalizeLifecycle(sink, state)

	buildSnap := p.BuildStateSnapshot
	if buildSnap == nil && p.IsInternalStateKey != nil {
		isInternal := p.IsInternalStateKey
		buildSnap = func(sess session.Session, reqState map[string]any) map[string]any {
			return BuildStateSnapshot(sess, reqState, isInternal)
		}
	}
	if state.RunCtx != nil && state.UserID != "" && p.LoadSessionForSnapshot != nil {
		if sess, ok, err := p.LoadSessionForSnapshot(state.RunCtx, state.RootAppName, state.UserID, state.ThreadID); err == nil && ok {
			if buildSnap != nil {
				EmitStateSnapshotIfNonEmpty(sink, withNodeOutputs(buildSnap(sess, state.ReqState), state))
			}
			if p.BuildMessagesSnapshot != nil {
				if msgs, err := p.BuildMessagesSnapshot(state.RunCtx, sess); err != nil {
					log.Printf("agui: failed to build messages snapshot for interrupt: %v", err)
				} else {
					EmitMessagesSnapshotIfNonEmpty(sink, msgs)
				}
			}
		} else if len(state.ReqState) > 0 && buildSnap != nil {
			EmitStateSnapshotIfNonEmpty(sink, withNodeOutputs(buildSnap(nil, state.ReqState), state))
		}
	}

	sink.Emit(events.NewRunFinishedEventWithOptions(
		state.ThreadID,
		state.RunID,
		events.WithInterruptOutcome(intrs),
	))
	if sink.Err() != nil {
		return sink.Err()
	}
	state.EmittedInterrupts = append(state.EmittedInterrupts, intrs...)
	state.RunFinalized = true
	return nil
}

// emitPredictStateIfConfigured emits a "PredictState" CustomEvent when the
// tool name matches a configured PredictStateMapping and hasn't been emitted
// for this tool yet in this run.
func emitPredictStateIfConfigured(sink eventSink, state *State, toolName string) {
	mappings, ok := state.PredictStateMappings[toolName]
	if !ok || len(mappings) == 0 {
		return
	}
	if state.EmittedPredictStateTools == nil {
		state.EmittedPredictStateTools = make(map[string]bool)
	}
	if state.EmittedPredictStateTools[toolName] {
		return
	}
	state.EmittedPredictStateTools[toolName] = true

	payload := make([]map[string]string, len(mappings))
	for i, m := range mappings {
		payload[i] = map[string]string{
			"state_key":     m.StateKey,
			"tool":          m.Tool,
			"tool_argument": m.ToolArgument,
		}
	}
	sink.Emit(events.NewCustomEvent("PredictState", events.WithValue(payload)))
}

// EscapeJSONPointer escapes a key for use in a JSON Pointer path (RFC 6901).
func EscapeJSONPointer(key string) string {
	key = strings.ReplaceAll(key, "~", "~0")
	key = strings.ReplaceAll(key, "/", "~1")
	return key
}

// MarshalPooled serializes v to JSON using a pooled buffer.
func MarshalPooled(v any) (string, error) {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	defer bufPool.Put(buf)
	if err := json.NewEncoder(buf).Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

// ExtractToolConfirmation reads toolConfirmation from an adk_request_confirmation call.
func ExtractToolConfirmation(fc *genai.FunctionCall) (toolconfirmation.ToolConfirmation, error) {
	if fc == nil || fc.Args == nil {
		return toolconfirmation.ToolConfirmation{}, fmt.Errorf("function call or args is nil")
	}
	raw, ok := fc.Args["toolConfirmation"]
	if !ok {
		return toolconfirmation.ToolConfirmation{}, fmt.Errorf("toolConfirmation missing from confirmation call")
	}

	switch v := raw.(type) {
	case *toolconfirmation.ToolConfirmation:
		if v != nil {
			return *v, nil
		}
		return toolconfirmation.ToolConfirmation{}, fmt.Errorf("toolConfirmation is nil")
	case toolconfirmation.ToolConfirmation:
		return v, nil
	case map[string]any:
		return decodeToolConfirmationMap(v)
	}

	b, err := json.Marshal(raw)
	if err != nil {
		return toolconfirmation.ToolConfirmation{}, fmt.Errorf("marshal toolConfirmation: %w", err)
	}
	var tc toolconfirmation.ToolConfirmation
	if err := json.Unmarshal(b, &tc); err != nil {
		return toolconfirmation.ToolConfirmation{}, fmt.Errorf("unmarshal toolConfirmation: %w", err)
	}
	return tc, nil
}

func decodeToolConfirmationMap(m map[string]any) (toolconfirmation.ToolConfirmation, error) {
	b, err := json.Marshal(m)
	if err != nil {
		return toolconfirmation.ToolConfirmation{}, err
	}
	var tc toolconfirmation.ToolConfirmation
	if err := json.Unmarshal(b, &tc); err != nil {
		return toolconfirmation.ToolConfirmation{}, err
	}
	return tc, nil
}
