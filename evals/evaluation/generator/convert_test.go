package generator_test

import (
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/generator"
	"go.alis.build/adk/launchers/evals/evaluation/models"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func buildEvent(t *testing.T, author string, parts []*genai.Part, invocationID string) *session.Event {
	t.Helper()
	ev := session.NewEvent(t.Context(), invocationID)
	ev.Author = author
	ev.Content = &genai.Content{Parts: parts}
	return ev
}

func TestConvertEventsEmpty(t *testing.T) {
	inv := generator.ConvertEventsToEvalInvocations(nil, nil)
	if len(inv) != 0 {
		t.Fatalf("len = %d", len(inv))
	}
}

func TestConvertSingleTurnTextOnly(t *testing.T) {
	events := []*session.Event{
		buildEvent(t, "user", []*genai.Part{{Text: "Hello"}}, "inv1"),
		buildEvent(t, "agent", []*genai.Part{{Text: "Hi there!"}}, "inv1"),
	}
	inv := generator.ConvertEventsToEvalInvocations(events, nil)
	if len(inv) != 1 {
		t.Fatalf("len = %d", len(inv))
	}
	if inv[0].InvocationID != "inv1" {
		t.Fatalf("id = %q", inv[0].InvocationID)
	}
	if inv[0].UserContent.Parts[0].Text != "Hello" {
		t.Fatalf("user = %q", inv[0].UserContent.Parts[0].Text)
	}
	if inv[0].FinalResponse == nil || inv[0].FinalResponse.Parts[0].Text != "Hi there!" {
		t.Fatalf("final = %+v", inv[0].FinalResponse)
	}
	// adk-python 2d428ead3 keeps the final event for its model usage, without
	// repeating its text.
	events2 := invocationEvents(t, inv[0])
	if len(events2) != 1 || events2[0].Content != nil {
		t.Fatalf("invocation events = %+v, want the final event with nil content", events2)
	}
}

func TestConvertSingleTurnToolCall(t *testing.T) {
	events := []*session.Event{
		buildEvent(t, "user", []*genai.Part{{Text: "what is the weather?"}}, "inv1"),
		buildEvent(t, "agent", []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "get_weather"}}}, "inv1"),
	}
	inv := generator.ConvertEventsToEvalInvocations(events, nil)
	if len(inv) != 1 {
		t.Fatalf("len = %d", len(inv))
	}
	if inv[0].FinalResponse != nil {
		t.Fatalf("expected nil final response, got %+v", inv[0].FinalResponse)
	}
	eventsData, ok := inv[0].IntermediateData.AsInvocationEvents()
	if !ok || len(eventsData.InvocationEvents) != 1 {
		t.Fatalf("intermediate = %+v", inv[0].IntermediateData.Value())
	}
	if eventsData.InvocationEvents[0].Content.Parts[0].FunctionCall.Name != "get_weather" {
		t.Fatalf("fc = %+v", eventsData.InvocationEvents[0].Content.Parts[0].FunctionCall)
	}
}

func TestConvertMultiTurn(t *testing.T) {
	events := []*session.Event{
		buildEvent(t, "user", []*genai.Part{{Text: "Hello"}}, "inv1"),
		buildEvent(t, "agent", []*genai.Part{{Text: "Hi there!"}}, "inv1"),
		buildEvent(t, "user", []*genai.Part{{Text: "How are you?"}}, "inv2"),
		buildEvent(t, "agent", []*genai.Part{{Text: "I am fine."}}, "inv2"),
	}
	inv := generator.ConvertEventsToEvalInvocations(events, nil)
	if len(inv) != 2 {
		t.Fatalf("len = %d", len(inv))
	}
	if inv[0].UserContent.Parts[0].Text != "Hello" || inv[1].UserContent.Parts[0].Text != "How are you?" {
		t.Fatalf("users = %q, %q", inv[0].UserContent.Parts[0].Text, inv[1].UserContent.Parts[0].Text)
	}
}

// Mirrors adk-python test_evaluation_generator.py
// test_convert_preserves_grounding_metadata_from_final_response: the final
// response's grounding metadata reaches the evaluators, without its text.
func TestConvertKeepsFinalResponseGroundingMetadata(t *testing.T) {
	agentEvent := buildEvent(t, "agent", []*genai.Part{{Text: "Here are sources."}}, "inv1")
	agentEvent.GroundingMetadata = &genai.GroundingMetadata{WebSearchQueries: []string{"recent AI news"}}
	events := []*session.Event{
		buildEvent(t, "user", []*genai.Part{{Text: "What's new in AI?"}}, "inv1"),
		agentEvent,
	}

	inv := generator.ConvertEventsToEvalInvocations(events, nil)
	if len(inv) != 1 {
		t.Fatalf("invocations = %d, want 1", len(inv))
	}
	invEvents := invocationEvents(t, inv[0])
	if len(invEvents) != 1 {
		t.Fatalf("invocation events = %d, want 1", len(invEvents))
	}
	if invEvents[0].Content != nil {
		t.Errorf("content = %+v, want nil", invEvents[0].Content)
	}
	gm := invEvents[0].GroundingMetadata
	if gm == nil || len(gm.WebSearchQueries) != 1 || gm.WebSearchQueries[0] != "recent AI news" {
		t.Errorf("grounding metadata = %+v, want web search query %q", gm, "recent AI news")
	}
}

// An event with grounding metadata and no parts is still recorded.
func TestConvertKeepsGroundingOnlyEvent(t *testing.T) {
	grounding := session.NewEvent(t.Context(), "inv1")
	grounding.Author = "agent"
	grounding.GroundingMetadata = &genai.GroundingMetadata{WebSearchQueries: []string{"q"}}
	events := []*session.Event{
		buildEvent(t, "user", []*genai.Part{{Text: "Hi"}}, "inv1"),
		grounding,
		buildEvent(t, "agent", []*genai.Part{{Text: "Done."}}, "inv1"),
	}

	invEvents := invocationEvents(t, generator.ConvertEventsToEvalInvocations(events, nil)[0])
	// The final event follows it, kept for its usage without its text
	// (adk-python 2d428ead3).
	if len(invEvents) != 2 || invEvents[0].GroundingMetadata == nil || invEvents[1].Content != nil {
		t.Fatalf("invocation events = %+v, want the grounding-only event then the final event without content", invEvents)
	}
}

func invocationEvents(t *testing.T, inv models.Invocation) []models.InvocationEvent {
	t.Helper()
	data, ok := inv.IntermediateData.AsInvocationEvents()
	if !ok {
		t.Fatalf("intermediate data = %+v, want invocation events", inv.IntermediateData.Value())
	}
	return data.InvocationEvents
}

// Mirrors adk-python test_efficiency_evaluators.py:266: each model event's
// usage and model version reach the invocation events, the final one kept
// with nil content.
func TestConvertRecordsUsageAndModelVersion(t *testing.T) {
	call := buildEvent(t, "agent", []*genai.Part{{FunctionCall: &genai.FunctionCall{Name: "t1", Args: map[string]any{}}}}, "inv1")
	call.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 60, CandidatesTokenCount: 40}
	call.ModelVersion = "gemini-test"
	final := buildEvent(t, "agent", []*genai.Part{{Text: "done"}}, "inv1")
	final.UsageMetadata = &genai.GenerateContentResponseUsageMetadata{PromptTokenCount: 200, CandidatesTokenCount: 50}
	final.ModelVersion = "gemini-test"
	events := []*session.Event{buildEvent(t, "user", []*genai.Part{{Text: "hi"}}, "inv1"), call, final}

	inv := generator.ConvertEventsToEvalInvocations(events, nil)

	got := invocationEvents(t, inv[0])
	if len(got) != 2 {
		t.Fatalf("invocation events = %+v, want 2", got)
	}
	for i, want := range [][2]int32{{60, 40}, {200, 50}} {
		u := got[i].UsageMetadata
		if u == nil || u.PromptTokenCount != want[0] || u.CandidatesTokenCount != want[1] || got[i].ModelVersion != "gemini-test" {
			t.Errorf("event %d usage = %+v, model %q; want %v from gemini-test", i, u, got[i].ModelVersion, want)
		}
	}
	if got[1].Content != nil {
		t.Errorf("final event content = %+v, want nil", got[1].Content)
	}
	if inv[0].FinalResponse == nil || inv[0].FinalResponse.Parts[0].Text != "done" {
		t.Errorf("final response = %+v, want done", inv[0].FinalResponse)
	}
}
