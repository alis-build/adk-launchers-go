package stream

import (
	"reflect"
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func TestNodeProvenanceFrom(t *testing.T) {
	t.Run("nil event carries no provenance", func(t *testing.T) {
		if _, ok := NodeProvenanceFrom(nil); ok {
			t.Error("NodeProvenanceFrom(nil) ok = true, want false")
		}
	})

	t.Run("nil NodeInfo means the event is not from a workflow", func(t *testing.T) {
		// ADK's documented invariant: readers test the pointer, not its
		// contents. Routes on a non-workflow event are not attribution.
		ev := &session.Event{Routes: []string{"next"}}
		if _, ok := NodeProvenanceFrom(ev); ok {
			t.Error("NodeProvenanceFrom(no NodeInfo) ok = true, want false")
		}
	})

	t.Run("reads every provenance field", func(t *testing.T) {
		ev := &session.Event{
			NodeInfo: &session.NodeInfo{
				Path:            "review/approve@run-1",
				MessageAsOutput: true,
				OutputFor:       []string{"review", "review/approve@run-1"},
			},
			Routes: []string{"publish", "reject"},
		}

		got, ok := NodeProvenanceFrom(ev)
		if !ok {
			t.Fatal("NodeProvenanceFrom() ok = false, want true")
		}
		want := NodeProvenance{
			Path:            "review/approve@run-1",
			MessageAsOutput: true,
			OutputFor:       []string{"review", "review/approve@run-1"},
			Routes:          []string{"publish", "reject"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("NodeProvenanceFrom() = %+v, want %+v", got, want)
		}
	})

	t.Run("empty path is still provenance when other fields are set", func(t *testing.T) {
		// ADK nils an all-zero NodeInfo only when decoding JSON, so an
		// in-process event can carry a non-nil NodeInfo with an empty Path.
		ev := &session.Event{NodeInfo: &session.NodeInfo{MessageAsOutput: true}}
		got, ok := NodeProvenanceFrom(ev)
		if !ok {
			t.Fatal("NodeProvenanceFrom() ok = false, want true")
		}
		if got.Path != "" {
			t.Errorf("Path = %q, want empty", got.Path)
		}
		if !got.MessageAsOutput {
			t.Error("MessageAsOutput = false, want true")
		}
	})
}

func TestNodeProvenanceStepName(t *testing.T) {
	tests := []struct {
		name   string
		prov   NodeProvenance
		author string
		want   string
	}{
		{
			name:   "path names the step",
			prov:   NodeProvenance{Path: "review/approve@run-1"},
			author: "reviewer",
			want:   "review/approve@run-1",
		},
		{
			// Top-level static nodes have no path; the agent name is what
			// identifies them.
			name:   "empty path falls back to the author",
			prov:   NodeProvenance{},
			author: "reviewer",
			want:   "reviewer",
		},
		{
			name:   "no path and no author names nothing",
			prov:   NodeProvenance{},
			author: "",
			want:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.prov.StepName(tt.author); got != tt.want {
				t.Errorf("StepName(%q) = %q, want %q", tt.author, got, tt.want)
			}
		})
	}
}

func TestNodeProvenanceOutputPaths(t *testing.T) {
	t.Run("OutputFor fans the output out to a delegation chain", func(t *testing.T) {
		// One event stands in for every delegating ancestor, so the output is
		// recorded against each rather than each level re-emitting it.
		prov := NodeProvenance{Path: "a/b", OutputFor: []string{"a", "a/b"}}
		want := []string{"a", "a/b"}
		if got := prov.OutputPaths(); !reflect.DeepEqual(got, want) {
			t.Errorf("OutputPaths() = %v, want %v", got, want)
		}
	})

	t.Run("without OutputFor the emitter owns its output", func(t *testing.T) {
		prov := NodeProvenance{Path: "a/b"}
		want := []string{"a/b"}
		if got := prov.OutputPaths(); !reflect.DeepEqual(got, want) {
			t.Errorf("OutputPaths() = %v, want %v", got, want)
		}
	})

	t.Run("no path and no OutputFor addresses nothing", func(t *testing.T) {
		if got := (NodeProvenance{}).OutputPaths(); len(got) != 0 {
			t.Errorf("OutputPaths() = %v, want empty", got)
		}
	})
}

func TestNodeOutputValue(t *testing.T) {
	t.Run("explicit Output wins", func(t *testing.T) {
		ev := &session.Event{
			NodeInfo: &session.NodeInfo{Path: "a", MessageAsOutput: true},
			Output:   map[string]any{"score": 9},
		}
		ev.Content = genai.NewContentFromText("ignored", genai.RoleModel)
		got, ok := NodeOutputValue(ev)
		if !ok {
			t.Fatal("NodeOutputValue() ok = false, want true")
		}
		if m, _ := got.(map[string]any); m["score"] != 9 {
			t.Errorf("NodeOutputValue() = %v, want the explicit Output", got)
		}
	})

	t.Run("MessageAsOutput derives the output from model text", func(t *testing.T) {
		ev := &session.Event{NodeInfo: &session.NodeInfo{Path: "a", MessageAsOutput: true}}
		ev.Content = genai.NewContentFromText("the verdict", genai.RoleModel)
		got, ok := NodeOutputValue(ev)
		if !ok {
			t.Fatal("NodeOutputValue() ok = false, want true")
		}
		if got != "the verdict" {
			t.Errorf("NodeOutputValue() = %v, want %q", got, "the verdict")
		}
	})

	t.Run("no Output and no MessageAsOutput yields nothing", func(t *testing.T) {
		ev := &session.Event{NodeInfo: &session.NodeInfo{Path: "a"}}
		ev.Content = genai.NewContentFromText("just chatter", genai.RoleModel)
		if _, ok := NodeOutputValue(ev); ok {
			t.Error("NodeOutputValue() ok = true, want false")
		}
	})

	t.Run("a non-workflow event has no node output", func(t *testing.T) {
		ev := &session.Event{Output: map[string]any{"score": 9}}
		if _, ok := NodeOutputValue(ev); ok {
			t.Error("NodeOutputValue() ok = true, want false without NodeInfo")
		}
	})

	t.Run("reasoning parts are not the node output", func(t *testing.T) {
		// Thought parts are how the node got there, not its result.
		ev := &session.Event{NodeInfo: &session.NodeInfo{Path: "a", MessageAsOutput: true}}
		ev.Content = &genai.Content{
			Role: string(genai.RoleModel),
			Parts: []*genai.Part{
				nil,
				{Text: "weighing options", Thought: true},
				{Text: "approved"},
			},
		}
		got, ok := NodeOutputValue(ev)
		if !ok {
			t.Fatal("NodeOutputValue() ok = false, want true")
		}
		if got != "approved" {
			t.Errorf("NodeOutputValue() = %v, want %q", got, "approved")
		}
	})

	t.Run("MessageAsOutput with no text yields nothing", func(t *testing.T) {
		ev := &session.Event{NodeInfo: &session.NodeInfo{Path: "a", MessageAsOutput: true}}
		if _, ok := NodeOutputValue(ev); ok {
			t.Error("NodeOutputValue() ok = true, want false for empty content")
		}
	})
}

func TestAnnotateNodeProvenance(t *testing.T) {
	nodeEv := func(path string, routes []string) *session.Event {
		return &session.Event{NodeInfo: &session.NodeInfo{Path: path}, Routes: routes}
	}

	t.Run("seeds metadata.adk when a handler left none", func(t *testing.T) {
		// Every handler today sets metadata.adk, so this covers the safety net
		// that keeps a future one from silently dropping attribution.
		intr := types.Interrupt{ID: "i-1"}
		annotateNodeProvenance(&intr, nodeEv("review", []string{"publish"}))

		adkMeta, ok := intr.Metadata["adk"].(map[string]any)
		if !ok {
			t.Fatal("metadata.adk was not created")
		}
		if adkMeta["nodePath"] != "review" {
			t.Errorf("nodePath = %v, want review", adkMeta["nodePath"])
		}
	})

	t.Run("merges into existing metadata without disturbing it", func(t *testing.T) {
		intr := types.Interrupt{
			ID:       "i-1",
			Metadata: map[string]any{"adk": map[string]any{"invocationId": "inv-1"}},
		}
		annotateNodeProvenance(&intr, nodeEv("review", nil))

		adkMeta, _ := intr.Metadata["adk"].(map[string]any)
		if adkMeta["invocationId"] != "inv-1" {
			t.Errorf("invocationId = %v, want it preserved", adkMeta["invocationId"])
		}
		if adkMeta["nodePath"] != "review" {
			t.Errorf("nodePath = %v, want review", adkMeta["nodePath"])
		}
		if _, present := adkMeta["routes"]; present {
			t.Error("routes present with none on the event, want omitted")
		}
	})

	t.Run("a non-workflow event adds nothing", func(t *testing.T) {
		intr := types.Interrupt{ID: "i-1"}
		annotateNodeProvenance(&intr, &session.Event{Routes: []string{"publish"}})
		if intr.Metadata != nil {
			t.Errorf("Metadata = %v, want untouched", intr.Metadata)
		}
	})

	t.Run("a node with nothing to say adds nothing", func(t *testing.T) {
		intr := types.Interrupt{ID: "i-1"}
		annotateNodeProvenance(&intr, &session.Event{NodeInfo: &session.NodeInfo{MessageAsOutput: true}})
		if intr.Metadata != nil {
			t.Errorf("Metadata = %v, want untouched", intr.Metadata)
		}
	})
}
