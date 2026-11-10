package simulation

import (
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/genai"
)

// UserSimulatorConfig is the user_simulator_config of an eval config or run
// request: an [LlmBackedUserSimulatorConfig] or an
// [LlmAudioUserSimulatorConfig], told apart by its "type".
type UserSimulatorConfig interface{ SimulatorType() string }

// SimulatorType returns "llm_backed".
func (LlmBackedUserSimulatorConfig) SimulatorType() string { return "llm_backed" }

// LlmAudioUserSimulatorConfig configures a simulator that writes each user
// turn as text and then speaks it (adk-python's LlmAudioUserSimulatorConfig).
type LlmAudioUserSimulatorConfig struct {
	LlmBackedUserSimulatorConfig
	// AudioModel is "cloud_tts" for Google Cloud Text-to-Speech or the name of
	// a Gemini TTS model.
	AudioModel string `json:"audioModel,omitempty"`
	// AudioModelConfiguration picks the voice through its speech config.
	AudioModelConfiguration *genai.GenerateContentConfig `json:"audioModelConfiguration,omitempty"`
	// IncludeTextWithAudio keeps the text part next to the audio part.
	IncludeTextWithAudio *bool `json:"includeTextWithAudio,omitempty"`
}

// SimulatorType returns "llm_audio".
func (LlmAudioUserSimulatorConfig) SimulatorType() string { return "llm_audio" }

// ParseUserSimulatorConfig decodes a raw user_simulator_config by its "type":
// missing or null is "llm_backed" (configs written before the field existed),
// "llm_audio" is the audio simulator, and anything else is an error. Keys may
// be camelCase or snake_case; a key that is absent takes adk-python's default.
func ParseUserSimulatorConfig(raw json.RawMessage) (UserSimulatorConfig, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("user_simulator_config: %w", err)
	}
	typ := "llm_backed"
	if v, ok := fields["type"]; ok && string(v) != "null" {
		if err := json.Unmarshal(v, &typ); err != nil {
			return nil, fmt.Errorf("user_simulator_config: type: %w", err)
		}
	}
	base := defaultLlmBackedConfig()
	if err := decodeFields(fields, map[[2]string]any{
		{"model", "model"}: &base.Model,
		{"maxAllowedInvocations", "max_allowed_invocations"}: &base.MaxAllowedInvocations,
		{"customInstructions", "custom_instructions"}:        &base.CustomInstructions,
		{"includeFunctionCalls", "include_function_calls"}:   &base.IncludeFunctionCalls,
	}); err != nil {
		return nil, err
	}
	switch typ {
	case "llm_backed":
		return base, nil
	case "llm_audio":
		includeText := true
		audio := LlmAudioUserSimulatorConfig{
			LlmBackedUserSimulatorConfig: base,
			AudioModel:                   "cloud_tts",
			IncludeTextWithAudio:         &includeText,
		}
		if err := decodeFields(fields, map[[2]string]any{
			{"audioModel", "audio_model"}:                            &audio.AudioModel,
			{"audioModelConfiguration", "audio_model_configuration"}: &audio.AudioModelConfiguration,
			{"includeTextWithAudio", "include_text_with_audio"}:      &audio.IncludeTextWithAudio,
		}); err != nil {
			return nil, err
		}
		if audio.AudioModelConfiguration == nil {
			audio.AudioModelConfiguration = defaultAudioModelConfiguration(audio.AudioModel)
		}
		return audio, nil
	default:
		return nil, fmt.Errorf("user_simulator_config: unknown type %q", typ)
	}
}

// decodeFields decodes each present, non-null key into its target. Keys are
// {camelCase, snake_case} pairs; the camelCase alias wins when both appear,
// as in pydantic.
func decodeFields(fields map[string]json.RawMessage, targets map[[2]string]any) error {
	for keys, target := range targets {
		for _, k := range []string{keys[1], keys[0]} {
			v, ok := fields[k]
			if !ok || string(v) == "null" {
				continue
			}
			if err := json.Unmarshal(v, target); err != nil {
				return fmt.Errorf("user_simulator_config: %s: %w", k, err)
			}
		}
	}
	return nil
}

// defaultAudioModelConfiguration is the audio config used when the run gives
// none. For a Gemini model it asks for audio output and leaves the voice to
// Gemini: adk-python's default voice, en-US-Studio-O, is a Cloud
// Text-to-Speech voice that Gemini TTS rejects with a 400, and Go has no
// cloud_tts engine for it to suit. Any other model gets adk-python's default,
// the prebuilt en-US-Studio-O voice speaking en-US.
func defaultAudioModelConfiguration(audioModel string) *genai.GenerateContentConfig {
	if strings.HasPrefix(strings.ToLower(audioModel), "gemini-") {
		return &genai.GenerateContentConfig{ResponseModalities: []string{string(genai.ModalityAudio)}}
	}
	return &genai.GenerateContentConfig{SpeechConfig: &genai.SpeechConfig{
		VoiceConfig:  &genai.VoiceConfig{PrebuiltVoiceConfig: &genai.PrebuiltVoiceConfig{VoiceName: "en-US-Studio-O"}},
		LanguageCode: "en-US",
	}}
}
