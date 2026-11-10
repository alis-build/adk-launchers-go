package simulation_test

import (
	"context"
	"iter"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"go.alis.build/adk/launchers/evals/evaluation/simulation"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// fakeAudioModel is an audio model that returns canned responses and
// records its requests.
type fakeAudioModel struct {
	responses []*model.LLMResponse
	requests  []*model.LLMRequest
}

func (m *fakeAudioModel) Name() string { return "fake-audio" }

func (m *fakeAudioModel) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	m.requests = append(m.requests, req)
	return func(yield func(*model.LLMResponse, error) bool) {
		for _, r := range m.responses {
			if !yield(r, nil) {
				return
			}
		}
	}
}

func audioResponse(data, mime string) *model.LLMResponse {
	return &model.LLMResponse{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{InlineData: &genai.Blob{Data: []byte(data), MIMEType: mime}}}}}
}

// cannedText is a text simulator that always returns msg.
type cannedText struct{ msg simulation.NextUserMessage }

func (c cannedText) GetNextUserMessage(context.Context, []*session.Event) (simulation.NextUserMessage, error) {
	return c.msg, nil
}

func textTurn(text string) simulation.NextUserMessage {
	return simulation.NextUserMessage{Status: simulation.StatusSuccess, UserMessage: genai.NewContentFromText(text, genai.RoleUser)}
}

func audioConfig(t *testing.T, raw string) simulation.LlmAudioUserSimulatorConfig {
	t.Helper()
	cfg, err := simulation.ParseUserSimulatorConfig([]byte(raw))
	if err != nil {
		t.Fatalf("ParseUserSimulatorConfig: %v", err)
	}
	return cfg.(simulation.LlmAudioUserSimulatorConfig)
}

// Mirrors adk-python test_llm_audio_user_simulator.py:190.
func TestAudioSimulatorReturnsTextAndAudio(t *testing.T) {
	m := &fakeAudioModel{responses: []*model.LLMResponse{audioResponse("WAV", "audio/pcm;rate=16000")}}
	cfg := audioConfig(t, `{"type":"llm_audio","audioModel":"my-tts"}`)
	got, err := simulation.NewAudioUserSimulator(cfg, cannedText{textTurn("Book me a flight.")}, m).GetNextUserMessage(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetNextUserMessage: %v", err)
	}
	p := got.UserMessage.Parts
	if got.Status != simulation.StatusSuccess || got.UserMessage.Role != genai.RoleUser || len(p) != 2 || p[0].Text != "Book me a flight." ||
		p[1].InlineData == nil || string(p[1].InlineData.Data) != "WAV" || p[1].InlineData.MIMEType != "audio/pcm;rate=16000" {
		t.Errorf("message = %+v", got.UserMessage)
	}
	if len(m.requests) != 1 || m.requests[0].Model != "my-tts" || !reflect.DeepEqual(m.requests[0].Config, cfg.AudioModelConfiguration) ||
		len(m.requests[0].Contents) != 1 || m.requests[0].Contents[0].Parts[0].Text != "Book me a flight." {
		t.Errorf("audio requests = %+v", m.requests)
	}
}

// Mirrors adk-python test_llm_audio_user_simulator.py:213.
func TestAudioSimulatorAudioOnly(t *testing.T) {
	m := &fakeAudioModel{responses: []*model.LLMResponse{audioResponse("WAV", "audio/pcm;rate=16000")}}
	got, err := simulation.NewAudioUserSimulator(audioConfig(t, `{"type":"llm_audio","includeTextWithAudio":false}`), cannedText{textTurn("hi")}, m).
		GetNextUserMessage(context.Background(), nil)
	if err != nil || len(got.UserMessage.Parts) != 1 || string(got.UserMessage.Parts[0].InlineData.Data) != "WAV" {
		t.Errorf("message = %+v, err = %v; want one audio part", got.UserMessage, err)
	}
}

// Mirrors adk-python test_llm_audio_user_simulator.py:244 and :257.
func TestAudioSimulatorPassesThroughWithoutText(t *testing.T) {
	for name, in := range map[string]simulation.NextUserMessage{
		"turn limit": {Status: simulation.StatusTurnLimitReached},
		"empty text": {Status: simulation.StatusSuccess, UserMessage: &genai.Content{Role: genai.RoleUser}},
	} {
		m := &fakeAudioModel{}
		got, err := simulation.NewAudioUserSimulator(audioConfig(t, `{"type":"llm_audio"}`), cannedText{in}, m).GetNextUserMessage(context.Background(), nil)
		if err != nil || got.Status != in.Status || got.UserMessage != in.UserMessage || len(m.requests) != 0 {
			t.Errorf("%s: got %+v, err %v, %d audio calls; want it unchanged and none", name, got, err, len(m.requests))
		}
	}
}

// Mirrors adk-python test_llm_audio_user_simulator.py:293 and :309.
func TestAudioSimulatorAudioErrors(t *testing.T) {
	for want, resp := range map[string]*model.LLMResponse{
		"Audio generation failed: SAFETY — blocked": {ErrorCode: "SAFETY", ErrorMessage: "blocked"},
		"Audio model returned no audio data":        {Content: genai.NewContentFromText("no audio", genai.RoleModel)},
	} {
		m := &fakeAudioModel{responses: []*model.LLMResponse{resp}}
		_, err := simulation.NewAudioUserSimulator(audioConfig(t, `{"type":"llm_audio"}`), cannedText{textTurn("hi")}, m).GetNextUserMessage(context.Background(), nil)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("error = %v, want %q", err, want)
		}
	}
}

// Audio split over several responses is joined, and the last MIME type
// decides the source rate (24 kHz here, so 600 samples become 400).
func TestAudioSimulatorJoinsAudioAndResamples(t *testing.T) {
	half := string(ramp(300))
	m := &fakeAudioModel{responses: []*model.LLMResponse{audioResponse(half, ""), audioResponse(half, "audio/pcm;rate=24000")}}
	got, err := simulation.NewAudioUserSimulator(audioConfig(t, `{"type":"llm_audio","includeTextWithAudio":false}`), cannedText{textTurn("hi")}, m).
		GetNextUserMessage(context.Background(), nil)
	if err != nil || len(got.UserMessage.Parts[0].InlineData.Data) != 800 || got.UserMessage.Parts[0].InlineData.MIMEType != simulation.LiveInputMIMEType {
		t.Errorf("message = %+v, err = %v; want 400 samples at 16 kHz", got.UserMessage, err)
	}
}

func staticCase() models.EvalCase {
	return models.EvalCase{EvalID: "c1", Conversation: []models.Invocation{{UserContent: genai.NewContentFromText("static hi", genai.RoleUser)}}}
}

func scenarioCase() models.EvalCase {
	return models.EvalCase{EvalID: "c1", ConversationScenario: &models.ConversationScenario{StartingPrompt: "start", ConversationPlan: "plan"}}
}

// resolveTo is a ResolveModel that returns m for every name, recording them.
func resolveTo(m model.LLM, names *[]string) func(context.Context, string) (model.LLM, error) {
	return func(_ context.Context, name string) (model.LLM, error) {
		*names = append(*names, name)
		return m, nil
	}
}

// Mirrors adk-python test_user_simulator_provider.py:58.
func TestProviderWrapsStaticCaseWithAudio(t *testing.T) {
	m := &fakeAudioModel{responses: []*model.LLMResponse{audioResponse("WAV", "audio/pcm;rate=16000")}}
	var names []string
	p := simulation.UserSimulatorProvider{SimulatorConfig: audioConfig(t, `{"type":"llm_audio"}`), ResolveModel: resolveTo(m, &names)}
	sim, err := p.Provide(context.Background(), staticCase())
	if err != nil {
		t.Fatalf("Provide: %v", err)
	}
	if _, ok := sim.(*simulation.AudioUserSimulator); !ok || len(names) != 1 || names[0] != "cloud_tts" {
		t.Fatalf("simulator = %T, resolved %v; want an audio simulator over cloud_tts", sim, names)
	}
	got, err := sim.GetNextUserMessage(context.Background(), nil)
	if err != nil || len(got.UserMessage.Parts) != 2 || got.UserMessage.Parts[0].Text != "static hi" || got.UserMessage.Parts[1].InlineData == nil {
		t.Errorf("first turn = %+v, err = %v; want the static text plus audio", got.UserMessage, err)
	}
}

// recordingFactory is an LLM simulator factory that records its config and
// returns a canned text simulator.
type recordingFactory struct {
	cfg *simulation.BaseUserSimulatorConfig
}

func (f *recordingFactory) NewLlmBackedSimulator(cfg simulation.BaseUserSimulatorConfig, _ *models.ConversationScenario) (simulation.UserSimulator, error) {
	f.cfg = &cfg
	return cannedText{textTurn("scenario hi")}, nil
}

// Mirrors adk-python test_user_simulator_provider.py:78.
func TestProviderWrapsScenarioWithAudio(t *testing.T) {
	m := &fakeAudioModel{responses: []*model.LLMResponse{audioResponse("WAV", "audio/pcm;rate=16000")}}
	var names []string
	f := &recordingFactory{}
	p := simulation.UserSimulatorProvider{LLM: f, SimulatorConfig: audioConfig(t, `{"type":"llm_audio","maxAllowedInvocations":7}`), ResolveModel: resolveTo(m, &names)}
	sim, err := p.Provide(context.Background(), scenarioCase())
	if err != nil {
		t.Fatalf("Provide: %v", err)
	}
	if _, ok := sim.(*simulation.AudioUserSimulator); !ok {
		t.Fatalf("simulator = %T, want an audio simulator", sim)
	}
	if f.cfg == nil || f.cfg.MaxInvocations == nil || *f.cfg.MaxInvocations != 7 {
		t.Errorf("text simulator config = %+v, want max invocations 7", f.cfg)
	}
	if got, err := sim.GetNextUserMessage(context.Background(), nil); err != nil || got.UserMessage.Parts[0].Text != "scenario hi" {
		t.Errorf("first turn = %+v, err = %v", got.UserMessage, err)
	}
}

func TestProviderReportsUnresolvableAudioModel(t *testing.T) {
	p := simulation.UserSimulatorProvider{
		SimulatorConfig: audioConfig(t, `{"type":"llm_audio"}`),
		ResolveModel:    func(context.Context, string) (model.LLM, error) { return nil, context.DeadlineExceeded },
	}
	_, err := p.Provide(context.Background(), staticCase())
	if err == nil || !strings.Contains(err.Error(), `user_simulator_config.audio_model "cloud_tts"`) {
		t.Errorf("error = %v, want one naming the audio model", err)
	}
}

// An llm_backed config keeps today's routing: static cases replay as text.
func TestProviderLlmBackedConfigKeepsStatic(t *testing.T) {
	p := simulation.UserSimulatorProvider{SimulatorConfig: simulation.LlmBackedUserSimulatorConfig{}}
	sim, err := p.Provide(context.Background(), staticCase())
	if _, ok := sim.(*simulation.StaticUserSimulator); err != nil || !ok {
		t.Errorf("simulator = %T, err = %v; want a static simulator", sim, err)
	}
}

// headerWritingModel writes into each request's config as ADK's gemini model
// does (model/gemini/gemini.go: HTTPOptions and its headers), then speaks.
type headerWritingModel struct{}

func (headerWritingModel) Name() string { return "header-writer" }

func (headerWritingModel) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	if req.Config.HTTPOptions == nil {
		req.Config.HTTPOptions = &genai.HTTPOptions{}
	}
	if req.Config.HTTPOptions.Headers == nil {
		req.Config.HTTPOptions.Headers = make(http.Header)
	}
	req.Config.HTTPOptions.Headers.Set("x-goog-api-client", "test")
	return func(yield func(*model.LLMResponse, error) bool) {
		yield(audioResponse("WAV", "audio/pcm;rate=16000"), nil)
	}
}

// Cases run concurrently over one config; each audio request must get its
// own copy (run with -race; sharing it is a concurrent map write).
func TestAudioSimulatorConcurrentCasesDoNotShareConfig(t *testing.T) {
	cfg := audioConfig(t, `{"type":"llm_audio","audioModelConfiguration":{"httpOptions":{"headers":{"X-Base":["1"]}}}}`)
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := simulation.NewAudioUserSimulator(cfg, cannedText{textTurn("hi")}, headerWritingModel{}).GetNextUserMessage(context.Background(), nil); err != nil {
				t.Errorf("GetNextUserMessage: %v", err)
			}
		}()
	}
	wg.Wait()
	if h := cfg.AudioModelConfiguration.HTTPOptions.Headers; h.Get("x-goog-api-client") != "" || h.Get("X-Base") != "1" {
		t.Errorf("shared config headers = %v, want them untouched", h)
	}
}

// recordingGenerator is a text-simulator model that records the model names
// it is asked for and replies "follow up".
type recordingGenerator struct {
	mu     sync.Mutex
	models []string
}

func (g *recordingGenerator) GenerateContent(_ context.Context, model, _ string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.models = append(g.models, model)
	return "follow up", nil
}

// A run's user_simulator_config sets the scenario's text simulator, for
// both config types (adk-python user_simulator_provider.py builds it from
// the audio config's text fields).
func TestProviderPassesRunTextConfigToFactory(t *testing.T) {
	for _, raw := range []string{
		`{"type":"llm_backed","model":"sim-model","maxAllowedInvocations":1}`,
		`{"type":"llm_audio","model":"sim-model","maxAllowedInvocations":1}`,
	} {
		cfg, err := simulation.ParseUserSimulatorConfig([]byte(raw))
		if err != nil {
			t.Fatalf("ParseUserSimulatorConfig: %v", err)
		}
		gen := &recordingGenerator{}
		p := simulation.UserSimulatorProvider{
			LLM:             &simulation.DefaultLlmBackedFactory{Config: simulation.LlmBackedUserSimulatorConfig{Model: "factory-model"}, Generator: gen},
			SimulatorConfig: cfg,
			ResolveModel:    func(context.Context, string) (model.LLM, error) { return headerWritingModel{}, nil },
		}
		sim, err := p.Provide(context.Background(), scenarioCase())
		if err != nil {
			t.Fatalf("%s: Provide: %v", raw, err)
		}
		// The starting prompt is turn one; the limit of one stops turn two
		// before the model is asked.
		first, err1 := sim.GetNextUserMessage(context.Background(), nil)
		second, err2 := sim.GetNextUserMessage(context.Background(), nil)
		if err1 != nil || err2 != nil || first.Status != simulation.StatusSuccess || second.Status != simulation.StatusTurnLimitReached {
			t.Errorf("%s: turns = %v, %v (%v, %v); want success then turn limit", raw, first.Status, second.Status, err1, err2)
		}
		third, _ := (&simulation.UserSimulatorProvider{LLM: p.LLM, SimulatorConfig: simulation.LlmBackedUserSimulatorConfig{Model: "sim-model", MaxAllowedInvocations: 3}}).Provide(context.Background(), scenarioCase())
		_, _ = third.GetNextUserMessage(context.Background(), nil)
		_, _ = third.GetNextUserMessage(context.Background(), nil)
		if len(gen.models) != 1 || gen.models[0] != "sim-model" {
			t.Errorf("%s: text model calls = %v, want one to sim-model", raw, gen.models)
		}
	}
}
