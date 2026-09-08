package agui

import (
	"encoding/json"
	"testing"
)

// TestProtocolCapabilities covers FR5: discovery has to name what the launcher
// can now do, or a client has no way to know deltas and encrypted reasoning are
// coming.
func TestProtocolCapabilities(t *testing.T) {
	t.Run("defaults advertise both features", func(t *testing.T) {
		caps := DefaultInterruptCapabilities()
		if caps.Output == nil {
			t.Fatal("DefaultInterruptCapabilities() left Output nil")
		}
		if caps.Output.ActivityDeltas == nil || !*caps.Output.ActivityDeltas {
			t.Error("output.activityDeltas not advertised")
		}
		if caps.Output.EncryptedReasoning == nil || !*caps.Output.EncryptedReasoning {
			t.Error("output.encryptedReasoning not advertised")
		}
	})

	t.Run("merge leaves a host's explicit opt-out alone", func(t *testing.T) {
		// A host that turned a feature off must not have it turned back on.
		off := false
		caps := Capabilities{Output: &OutputCapabilities{ActivityDeltas: &off}}
		MergeProtocolCapabilities(&caps)
		if *caps.Output.ActivityDeltas {
			t.Error("MergeProtocolCapabilities overrode an explicit false")
		}
	})

	t.Run("serialized under output", func(t *testing.T) {
		data, err := json.Marshal(DefaultInterruptCapabilities())
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var doc struct {
			Output struct {
				ActivityDeltas     *bool `json:"activityDeltas"`
				EncryptedReasoning *bool `json:"encryptedReasoning"`
			} `json:"output"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if doc.Output.ActivityDeltas == nil || !*doc.Output.ActivityDeltas {
			t.Error("output.activityDeltas missing from the JSON document")
		}
		if doc.Output.EncryptedReasoning == nil || !*doc.Output.EncryptedReasoning {
			t.Error("output.encryptedReasoning missing from the JSON document")
		}
	})
}
