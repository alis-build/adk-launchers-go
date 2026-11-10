package simulation_test

import (
	"strings"
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/simulation"
	"google.golang.org/genai"
)

func parse(t *testing.T, raw string) simulation.UserSimulatorConfig {
	t.Helper()
	cfg, err := simulation.ParseUserSimulatorConfig([]byte(raw))
	if err != nil {
		t.Fatalf("ParseUserSimulatorConfig(%s): %v", raw, err)
	}
	return cfg
}

func asLlmBacked(t *testing.T, cfg simulation.UserSimulatorConfig) simulation.LlmBackedUserSimulatorConfig {
	t.Helper()
	c, ok := cfg.(simulation.LlmBackedUserSimulatorConfig)
	if !ok || c.SimulatorType() != "llm_backed" {
		t.Fatalf("config = %T, want llm_backed", cfg)
	}
	return c
}

// Mirrors adk-python test_eval_config.py:330.
func TestParseUserSimulatorConfigExplicitLlmBacked(t *testing.T) {
	c := asLlmBacked(t, parse(t, `{"type":"llm_backed","model":"my-model","maxAllowedInvocations":5}`))
	if c.Model != "my-model" || c.MaxAllowedInvocations != 5 {
		t.Errorf("config = %+v, want my-model and 5", c)
	}
}

// Mirrors adk-python test_eval_config.py:350.
func TestParseUserSimulatorConfigLlmAudio(t *testing.T) {
	cfg := parse(t, `{"type":"llm_audio","model":"my-model","maxAllowedInvocations":5}`)
	c, ok := cfg.(simulation.LlmAudioUserSimulatorConfig)
	if !ok || c.SimulatorType() != "llm_audio" {
		t.Fatalf("config = %T, want llm_audio", cfg)
	}
	if c.Model != "my-model" || c.MaxAllowedInvocations != 5 || c.AudioModel != "cloud_tts" || c.IncludeTextWithAudio == nil || !*c.IncludeTextWithAudio {
		t.Errorf("config = %+v, want my-model, 5, cloud_tts, include text", c)
	}
}

// adk-python _llm_audio_user_simulator.py:78-185 defaults.
func TestParseUserSimulatorConfigLlmAudioDefaults(t *testing.T) {
	c := parse(t, `{"type":"llm_audio"}`).(simulation.LlmAudioUserSimulatorConfig)
	if c.Model != "gemini-2.5-flash" || c.MaxAllowedInvocations != 20 || c.AudioModel != "cloud_tts" || c.IncludeTextWithAudio == nil || !*c.IncludeTextWithAudio {
		t.Errorf("config = %+v, want adk-python defaults", c)
	}
	sc := c.AudioModelConfiguration
	if sc == nil || sc.SpeechConfig == nil || sc.SpeechConfig.LanguageCode != "en-US" || sc.SpeechConfig.VoiceConfig == nil ||
		sc.SpeechConfig.VoiceConfig.PrebuiltVoiceConfig == nil || sc.SpeechConfig.VoiceConfig.PrebuiltVoiceConfig.VoiceName != "en-US-Studio-O" {
		t.Errorf("audio configuration = %+v, want en-US-Studio-O, en-US", sc)
	}
}

// Inner keys accept snake_case as well as camelCase.
func TestParseUserSimulatorConfigSnakeKeys(t *testing.T) {
	c := parse(t, `{"type":"llm_audio","max_allowed_invocations":3,"audio_model":"gemini-2.5-flash-preview-tts","include_text_with_audio":false,"custom_instructions":"ci","include_function_calls":true,"audio_model_configuration":{"responseModalities":["AUDIO"]}}`).(simulation.LlmAudioUserSimulatorConfig)
	if c.MaxAllowedInvocations != 3 || c.AudioModel != "gemini-2.5-flash-preview-tts" || c.IncludeTextWithAudio == nil || *c.IncludeTextWithAudio ||
		c.CustomInstructions != "ci" || !c.IncludeFunctionCalls || c.AudioModelConfiguration == nil || len(c.AudioModelConfiguration.ResponseModalities) != 1 {
		t.Errorf("config = %+v", c)
	}
}

// Mirrors adk-python test_eval_config.py:370.
func TestParseUserSimulatorConfigWithoutTypeIsLlmBacked(t *testing.T) {
	if c := asLlmBacked(t, parse(t, `{"model":"legacy-model"}`)); c.Model != "legacy-model" {
		t.Errorf("model = %q, want legacy-model", c.Model)
	}
}

// Mirrors adk-python test_eval_config.py:407.
func TestParseUserSimulatorConfigNullType(t *testing.T) {
	if c := asLlmBacked(t, parse(t, `{"type":null,"model":"explicit-null"}`)); c.Model != "explicit-null" {
		t.Errorf("model = %q, want explicit-null", c.Model)
	}
}

// Mirrors adk-python test_eval_config.py:426.
func TestParseUserSimulatorConfigUnknownType(t *testing.T) {
	_, err := simulation.ParseUserSimulatorConfig([]byte(`{"type":"typo_type_name"}`))
	if err == nil || !strings.Contains(err.Error(), "typo_type_name") {
		t.Errorf("error = %v, want one naming typo_type_name", err)
	}
}

// Found in the hand check: adk-python's default voice is a Cloud
// Text-to-Speech voice that Gemini TTS rejects, and Vertex AI rejects a
// Gemini TTS request with no voice, so a Gemini audio model with no
// configuration gets audio output in the Gemini voice Kore.
func TestParseUserSimulatorConfigGeminiAudioDefault(t *testing.T) {
	c := parse(t, `{"type":"llm_audio","audio_model":"gemini-2.5-flash-tts"}`).(simulation.LlmAudioUserSimulatorConfig)
	cfg := c.AudioModelConfiguration
	if cfg == nil || len(cfg.ResponseModalities) != 1 || cfg.ResponseModalities[0] != string(genai.ModalityAudio) ||
		cfg.SpeechConfig == nil || cfg.SpeechConfig.VoiceConfig == nil || cfg.SpeechConfig.VoiceConfig.PrebuiltVoiceConfig == nil ||
		cfg.SpeechConfig.VoiceConfig.PrebuiltVoiceConfig.VoiceName != "Kore" || cfg.SpeechConfig.LanguageCode != "" {
		t.Errorf("audio configuration = %+v, want AUDIO output in the Gemini voice Kore", cfg)
	}
	given := parse(t, `{"type":"llm_audio","audio_model":"gemini-2.5-flash-tts","audio_model_configuration":{"speechConfig":{"languageCode":"fr-FR"}}}`).(simulation.LlmAudioUserSimulatorConfig)
	if given.AudioModelConfiguration.SpeechConfig == nil || given.AudioModelConfiguration.SpeechConfig.LanguageCode != "fr-FR" || len(given.AudioModelConfiguration.ResponseModalities) != 0 {
		t.Errorf("given configuration = %+v, want it used as given", given.AudioModelConfiguration)
	}
}
