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
	if len(invEvents) != 1 || invEvents[0].GroundingMetadata == nil {
		t.Fatalf("invocation events = %+v, want the grounding-only event", invEvents)
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
