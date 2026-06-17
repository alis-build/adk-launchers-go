package agui

import (
	"context"
	"net/http"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/encoding/sse"
	"go.alis.build/adk/launchers/agui/internal/stream"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool/toolconfirmation"
	"google.golang.org/genai"
)

// Type aliases bridge internal/stream types into the agui package without exporting them.
// This lets executor.go and other agui files use short names (e.g. eventSink, yieldSink)
// while keeping the real implementations in a focused internal package.
type (
	eventSink      = stream.Sink
	emitter        = stream.WireEmitter
	yieldSink      = stream.YieldSink
	streamState    = stream.State
	eventCollector = stream.EventCollector
)

func newEmitter(ctx context.Context, w http.ResponseWriter, writer *sse.SSEWriter) *emitter {
	return stream.NewWireEmitter(ctx, w, writer)
}

func newYieldSink(yield func(events.Event, error) bool) *yieldSink {
	return stream.NewYieldSink(yield)
}

func newEventCollector(inner eventSink) *eventCollector {
	return stream.NewEventCollector(inner)
}

func sinkStopped(s eventSink) bool {
	ys, ok := s.(*yieldSink)
	return ok && ys.Stopped()
}

func (l *aguiLauncher) streamProcessor() *stream.Processor {
	return &stream.Processor{
		DefaultPartConverter:   l.config.genAIPartConverter,
		LoadSessionForSnapshot: l.loadSessionForSnapshot,
		BuildMessagesSnapshot:  l.buildMessagesSnapshot,
		IsInternalStateKey:     isInternalStateKey,
		BuildStateSnapshot: func(sess session.Session, reqState map[string]any) map[string]any {
			return stream.BuildStateSnapshot(sess, reqState, isInternalStateKey)
		},
	}
}

func (l *aguiLauncher) processEvent(sink eventSink, ev *session.Event, state *streamState, partConverter GenAIPartConverter) (bool, error) {
	return l.streamProcessor().ProcessEvent(sink, ev, state, partConverter)
}

func finalizeLifecycle(sink eventSink, state *streamState) {
	stream.FinalizeLifecycle(sink, state)
}

func emitStateSnapshotIfNonEmpty(sink eventSink, snapshot map[string]any) {
	stream.EmitStateSnapshotIfNonEmpty(sink, snapshot)
}

func emitMessagesSnapshotIfNonEmpty(sink eventSink, messages []types.Message) {
	stream.EmitMessagesSnapshotIfNonEmpty(sink, messages)
}

func escapeJSONPointer(key string) string {
	return stream.EscapeJSONPointer(key)
}

func marshalPooled(v any) (string, error) {
	return stream.MarshalPooled(v)
}

func extractToolConfirmation(fc *genai.FunctionCall) (toolconfirmation.ToolConfirmation, error) {
	return stream.ExtractToolConfirmation(fc)
}
