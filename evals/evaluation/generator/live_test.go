package generator

import (
	"bytes"
	"context"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/simulation"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// Mirrors adk-python test_evaluation_generator.py:905, :923 and :943.
func TestSendAudioToLive(t *testing.T) {
	sess := &fakeLiveSession{turns: make(chan struct{}, 1)}
	audio := make([]byte, 16000*2+8000)
	for i := range audio {
		audio[i] = byte(i)
	}
	content := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{Text: "ignored"}, {InlineData: &genai.Blob{Data: audio, MIMEType: "audio/pcm"}}}}
	if err := sendAudioToLive(sess, content); err != nil {
		t.Fatalf("sendAudioToLive: %v", err)
	}
	reqs := sess.recorded()
	if len(reqs) != 5 {
		t.Fatalf("%d requests, want start, three chunks, end", len(reqs))
	}
	if _, ok := reqs[0].RealtimeInput.(*genai.ActivityStart); !ok {
		t.Errorf("first request = %+v, want ActivityStart", reqs[0])
	}
	if _, ok := reqs[4].RealtimeInput.(*genai.ActivityEnd); !ok {
		t.Errorf("last request = %+v, want ActivityEnd", reqs[4])
	}
	var joined []byte
	for _, r := range reqs[1:4] {
		b, ok := r.RealtimeInput.(*genai.Blob)
		if !ok || b.MIMEType != "audio/pcm" || r.Content != nil {
			t.Fatalf("chunk request = %+v, want an audio/pcm blob", r)
		}
		joined = append(joined, b.Data...)
	}
	if !bytes.Equal(joined, audio) {
		t.Error("chunks do not concatenate to the input")
	}
}

func TestLiveRunConfigDisablesActivityDetection(t *testing.T) {
	cfg := liveRunConfig()
	if cfg.RealtimeInputConfig == nil || cfg.RealtimeInputConfig.AutomaticActivityDetection == nil || !cfg.RealtimeInputConfig.AutomaticActivityDetection.Disabled {
		t.Errorf("realtime input config = %+v, want automatic activity detection disabled", cfg.RealtimeInputConfig)
	}
}

// Mirrors adk-python test_evaluation_generator.py:857.
func TestLiveAudioTurnSendsOnlyAudioAndLogsFullContent(t *testing.T) {
	rt, sess := newFakeLiveRuntime(t)
	turn := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{
		{Text: "Book me a flight."},
		{InlineData: &genai.Blob{Data: []byte("WAV"), MIMEType: simulation.LiveInputMIMEType}},
	}}
	inv, err := (&Generator{Runtime: rt}).GenerateInferences(context.Background(), InferenceOptions{
		UserSimulator:      simulation.NewStaticUserSimulator([]models.Invocation{{UserContent: turn}}),
		UseLive:            true,
		LiveTimeoutSeconds: 5,
	})
	if err != nil {
		t.Fatalf("GenerateInferences: %v", err)
	}
	reqs := sess.recorded()
	if len(reqs) != 3 {
		t.Fatalf("requests = %+v, want start, blob, end", reqs)
	}
	for _, r := range reqs {
		if r.Content != nil {
			t.Errorf("request %+v sent content, want audio only", r)
		}
	}
	if b, ok := reqs[1].RealtimeInput.(*genai.Blob); !ok || string(b.Data) != "WAV" {
		t.Errorf("blob request = %+v, want WAV", reqs[1])
	}
	if len(inv) != 1 || inv[0].UserContent == nil || len(inv[0].UserContent.Parts) != 2 {
		t.Fatalf("invocations = %+v, want the user content with text and audio", inv)
	}
	if inv[0].FinalResponse == nil || inv[0].FinalResponse.Parts[0].Text != "reply" {
		t.Errorf("final response = %+v, want reply", inv[0].FinalResponse)
	}
}

func transcriptionEvent(t *testing.T, input, output string) *session.Event {
	t.Helper()
	ev := session.NewEvent(t.Context(), "inv1")
	ev.Author = "reply-agent"
	if input != "" {
		ev.InputTranscription = &genai.Transcription{Text: input}
	}
	if output != "" {
		ev.OutputTranscription = &genai.Transcription{Text: output}
	}
	return ev
}

// Mirrors adk-python test_evaluation_generator.py:348.
func TestNormalizeOutputTranscription(t *testing.T) {
	in := transcriptionEvent(t, "", "agent said")
	got := normalizeLiveTranscriptions([]*session.Event{in})
	if len(got) != 1 || got[0].Author != "reply-agent" || got[0].Content == nil || got[0].Content.Role != genai.RoleModel ||
		got[0].Content.Parts[0].Text != "agent said" || got[0].OutputTranscription != nil {
		t.Errorf("normalized = %+v", got[0])
	}
	if in.Content != nil || in.OutputTranscription == nil {
		t.Error("the input event was modified")
	}
}

// Mirrors adk-python test_evaluation_generator.py:363.
func TestNormalizeInputTranscription(t *testing.T) {
	got := normalizeLiveTranscriptions([]*session.Event{transcriptionEvent(t, "user said", "")})
	if len(got) != 1 || got[0].Content == nil || got[0].Content.Role != genai.RoleUser || got[0].Content.Parts[0].Text != "user said" || got[0].InputTranscription != nil {
		t.Errorf("normalized = %+v", got[0])
	}
}

// Mirrors adk-python test_partial_transcriptions_are_passed_through.
func TestNormalizeKeepsPartialTranscriptions(t *testing.T) {
	in := transcriptionEvent(t, "", "partial")
	in.Partial = true
	if got := normalizeLiveTranscriptions([]*session.Event{in}); len(got) != 1 || got[0] != in {
		t.Errorf("normalized = %+v, want the partial event unchanged", got)
	}
}

// The simulator sees transcripts as text: the history it is given is
// normalized.
func TestLiveSimulatorSeesTranscriptsAsText(t *testing.T) {
	rt, _ := newFakeLiveRuntime(t)
	sim := &historyRecorder{turns: []string{"one", "two"}}
	if _, err := (&Generator{Runtime: rt}).GenerateInferences(context.Background(), InferenceOptions{UserSimulator: sim, UseLive: true, LiveTimeoutSeconds: 5}); err != nil {
		t.Fatalf("GenerateInferences: %v", err)
	}
	var sawTranscript bool
	for _, ev := range sim.histories[1] {
		if ev.OutputTranscription != nil {
			t.Errorf("history has a raw transcription event: %+v", ev)
		}
		if ev.Content != nil && len(ev.Content.Parts) > 0 && ev.Content.Parts[0].Text == "transcribed reply" {
			sawTranscript = true
		}
	}
	if !sawTranscript {
		t.Error("second turn's history lacks the transcript as text")
	}
}

// historyRecorder replays turns as text and records each history it is given.
type historyRecorder struct {
	turns     []string
	histories [][]*session.Event
}

func (h *historyRecorder) GetNextUserMessage(_ context.Context, events []*session.Event) (simulation.NextUserMessage, error) {
	h.histories = append(h.histories, events)
	if len(h.histories) > len(h.turns) {
		return simulation.NextUserMessage{Status: simulation.StatusStopSignalDetected}, nil
	}
	return simulation.NextUserMessage{Status: simulation.StatusSuccess, UserMessage: genai.NewContentFromText(h.turns[len(h.histories)-1], genai.RoleUser)}, nil
}

var _ agent.LiveSession = (*fakeLiveSession)(nil)
