package agui

import (
	"encoding/json"
	"slices"
	"testing"
)

// TestInterruptReasonsAdvertised covers spec FR6: discovery must name the
// interrupt reasons this launcher can now produce, so a client knows to expect
// input_required and confirmation and not just tool_call.
func TestInterruptReasonsAdvertised(t *testing.T) {
	want := []string{"tool_call", "input_required", "confirmation"}

	t.Run("defaults list every supported reason", func(t *testing.T) {
		caps := DefaultInterruptCapabilities()
		if caps.HumanInTheLoop == nil {
			t.Fatal("DefaultInterruptCapabilities() left HumanInTheLoop nil")
		}
		if !slices.Equal(caps.HumanInTheLoop.InterruptReasons, want) {
			t.Errorf("InterruptReasons = %v, want %v", caps.HumanInTheLoop.InterruptReasons, want)
		}
	})

	t.Run("merge fills reasons when unset", func(t *testing.T) {
		caps := Capabilities{}
		MergeInterruptCapabilities(&caps)
		if !slices.Equal(caps.HumanInTheLoop.InterruptReasons, want) {
			t.Errorf("InterruptReasons = %v, want %v", caps.HumanInTheLoop.InterruptReasons, want)
		}
	})

	t.Run("merge leaves a host's own list alone", func(t *testing.T) {
		// A host with a custom classifier advertises its own reasons; the merge
		// must not append the defaults back over them.
		custom := []string{"acme.manager_approval"}
		caps := Capabilities{HumanInTheLoop: &HumanInTheLoopCapabilities{InterruptReasons: custom}}
		MergeInterruptCapabilities(&caps)
		if !slices.Equal(caps.HumanInTheLoop.InterruptReasons, custom) {
			t.Errorf("InterruptReasons = %v, want the host's %v", caps.HumanInTheLoop.InterruptReasons, custom)
		}
	})

	t.Run("reasons serialize under interruptReasons", func(t *testing.T) {
		data, err := json.Marshal(DefaultInterruptCapabilities())
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var doc struct {
			HumanInTheLoop struct {
				InterruptReasons []string `json:"interruptReasons"`
			} `json:"humanInTheLoop"`
		}
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !slices.Equal(doc.HumanInTheLoop.InterruptReasons, want) {
			t.Errorf("humanInTheLoop.interruptReasons = %v, want %v", doc.HumanInTheLoop.InterruptReasons, want)
		}
	})
}
