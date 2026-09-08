package stream

import (
	"encoding/base64"
	"reflect"
	"strconv"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	"google.golang.org/genai"
)

// activityKey identifies one activity surface within a run.
//
// Both parts are needed: one message id can carry several activity types, and
// one activity type can appear on several message ids, so keying on either
// alone would let unrelated surfaces patch each other.
type activityKey struct {
	MessageID    string
	ActivityType string
}

// RecordActivitySnapshot remembers the content last sent for an activity
// surface, so the next update for the same surface can be expressed as a patch
// against it.
//
// The content is deep-copied because a converter that keeps one running
// activity object and emits snapshots of it hands back the same map every
// time. Storing that by reference would leave the recorded "previous" snapshot
// aliasing the live one, so the next patch computation would diff a map
// against itself, produce no operations, and silently freeze the client's
// activity block at its first value.
func (s *State) RecordActivitySnapshot(messageID, activityType string, content any) {
	if s.ActivitySnapshots == nil {
		s.ActivitySnapshots = make(map[activityKey]any, 1)
	}
	s.ActivitySnapshots[activityKey{messageID, activityType}] = copyActivityContent(content)
}

// copyActivityContent deep-copies the JSON-shaped value an activity snapshot
// carries. Maps and slices are rebuilt; everything else is a scalar the patch
// computation only ever compares, never mutates.
func copyActivityContent(content any) any {
	switch v := content.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, val := range v {
			out[key] = copyActivityContent(val)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, val := range v {
			out[i] = copyActivityContent(val)
		}
		return out
	default:
		return content
	}
}

// LastActivitySnapshot returns the content last sent for an activity surface.
// The second result is false when nothing has been sent for it in this run.
func (s *State) LastActivitySnapshot(messageID, activityType string) (any, bool) {
	content, ok := s.ActivitySnapshots[activityKey{messageID, activityType}]
	return content, ok
}

// ClearActivitySnapshots drops all tracked activity content.
//
// Tracking is per run. Carrying it across runs would let the first activity of
// a new run go out as a patch against a surface the client has already torn
// down, leaving it with nothing to apply the patch to.
func (s *State) ClearActivitySnapshots() {
	clear(s.ActivitySnapshots)
}

// ComputeActivityPatch derives a JSON Patch turning prev into next. The second
// result is false when no patch can be built, in which case the caller sends a
// fresh snapshot instead.
//
// Operations are limited to add, replace and remove. Nothing here needs move or
// copy, and both invite subtle index bugs for a saving that does not matter at
// activity-payload sizes.
//
// Both sides must be JSON objects. The paths built here are rooted at object
// keys, so a scalar or array at the root has nothing to address; that is the
// unpatchable case rather than an error.
func ComputeActivityPatch(prev, next any) ([]events.JSONPatchOperation, bool) {
	prevObj, ok := prev.(map[string]any)
	if !ok {
		return nil, false
	}
	nextObj, ok := next.(map[string]any)
	if !ok {
		return nil, false
	}
	return appendObjectPatch(nil, "", prevObj, nextObj), true
}

// appendObjectPatch walks two objects at base, appending the operations that
// turn prev into next.
func appendObjectPatch(ops []events.JSONPatchOperation, base string, prev, next map[string]any) []events.JSONPatchOperation {
	for key, nextVal := range next {
		path := base + "/" + EscapeJSONPointer(key)
		prevVal, existed := prev[key]
		switch {
		case !existed:
			ops = append(ops, events.JSONPatchOperation{Op: "add", Path: path, Value: nextVal})
		case reflect.DeepEqual(prevVal, nextVal):
			// Unchanged: nothing to send, which is the whole point of a delta.
		default:
			ops = appendValuePatch(ops, path, prevVal, nextVal)
		}
	}
	for key := range prev {
		if _, present := next[key]; !present {
			ops = append(ops, events.JSONPatchOperation{Op: "remove", Path: base + "/" + EscapeJSONPointer(key)})
		}
	}
	return ops
}

// appendValuePatch appends the operations for one changed value at path.
//
// Objects and same-length arrays recurse so only the changed leaves travel. A
// length change replaces the array whole: diffing across it needs adds and
// removes at shifting indices, which is where hand-written patches go wrong, and
// the replacement is never larger than the array itself.
func appendValuePatch(ops []events.JSONPatchOperation, path string, prev, next any) []events.JSONPatchOperation {
	prevObj, prevIsObj := prev.(map[string]any)
	nextObj, nextIsObj := next.(map[string]any)
	if prevIsObj && nextIsObj {
		return appendObjectPatch(ops, path, prevObj, nextObj)
	}

	prevArr, prevIsArr := prev.([]any)
	nextArr, nextIsArr := next.([]any)
	if prevIsArr && nextIsArr && len(prevArr) == len(nextArr) {
		for i := range nextArr {
			if !reflect.DeepEqual(prevArr[i], nextArr[i]) {
				ops = appendValuePatch(ops, path+"/"+strconv.Itoa(i), prevArr[i], nextArr[i])
			}
		}
		return ops
	}

	return append(ops, events.JSONPatchOperation{Op: "replace", Path: path, Value: next})
}

// EmitConverterEvent relays an event produced by a host part converter,
// converting a repeated activity snapshot into a delta.
//
// Activity events reach the stream only this way: the launcher has no activity
// source of its own, so this relay is the one place a repeat can be recognised.
func EmitConverterEvent(sink eventSink, state *State, ev events.Event) {
	snap, ok := ev.(*events.ActivitySnapshotEvent)
	if !ok {
		sink.Emit(ev)
		return
	}
	emitActivityUpdate(sink, state, snap)
}

// emitActivityUpdate sends an activity update as a patch when the client
// already holds a snapshot for that surface, and in full otherwise.
//
// A repeat that changes nothing is dropped. The SDK rejects a delta with an
// empty patch ("patch field must contain at least one operation"), which is the
// protocol declining to carry no-ops, and the client's state already matches so
// a full snapshot would say nothing either.
func emitActivityUpdate(sink eventSink, state *State, snap *events.ActivitySnapshotEvent) {
	if prev, seen := state.LastActivitySnapshot(snap.MessageID, snap.ActivityType); seen {
		if patch, ok := ComputeActivityPatch(prev, snap.Content); ok {
			if len(patch) == 0 {
				return
			}
			sink.Emit(events.NewActivityDeltaEvent(snap.MessageID, snap.ActivityType, patch))
			state.RecordActivitySnapshot(snap.MessageID, snap.ActivityType, snap.Content)
			return
		}
	}
	sink.Emit(snap)
	state.RecordActivitySnapshot(snap.MessageID, snap.ActivityType, snap.Content)
}

// emitEncryptedReasoning emits the opaque reasoning blob a model returned
// alongside its thoughts, so the reasoning state survives to the client and back.
//
// ADK surfaces it as genai.Part.ThoughtSignature, raw bytes; AG-UI carries a
// string, so it is base64-encoded. The value is opaque to both: the launcher
// neither reads nor validates it.
//
// Called for every part, not only thought parts. ADK attaches the signature to
// whichever part ends the reasoning turn, and its stream aggregator strips it
// from flushed thought text and re-attaches it to the following function call
// (stream_aggregator.go). Keying emission off thought parts alone therefore
// loses the blob on exactly the tool-calling turns it exists to make resumable.
//
// The blob belongs inside the REASONING_START / REASONING_END bracket, so a
// bracket is opened when the carrying part did not open one itself. Callers
// emit before closing the bracket for the same reason. ADK partials carry
// accumulated state, so the same blob can arrive on several events for one
// message and is sent only when it changes.
func emitEncryptedReasoning(sink eventSink, state *State, part *genai.Part) {
	if len(part.ThoughtSignature) == 0 {
		return
	}
	encoded := base64.StdEncoding.EncodeToString(part.ThoughtSignature)
	if encoded == state.EmittedThoughtSignature {
		return
	}
	state.EmittedThoughtSignature = encoded
	openReasoningMessage(sink, state)
	sink.Emit(events.NewReasoningEncryptedValueEvent(
		events.ReasoningEncryptedValueSubtypeMessage,
		state.CurrentReasoningMessageID,
		encoded,
	))
}
