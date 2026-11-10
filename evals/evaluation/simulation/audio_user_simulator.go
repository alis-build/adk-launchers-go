package simulation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// AudioUserSimulator speaks the turns of a text simulator: each text turn is
// sent to an audio model, and the user message carries the speech (and,
// by default, the text). A port of adk-python's _LlmAudioUserSimulator.
type AudioUserSimulator struct {
	cfg   LlmAudioUserSimulatorConfig
	text  UserSimulator
	audio model.LLM
}

// NewAudioUserSimulator returns a simulator that speaks text's turns through
// audio, configured by cfg.
func NewAudioUserSimulator(cfg LlmAudioUserSimulatorConfig, text UserSimulator, audio model.LLM) *AudioUserSimulator {
	return &AudioUserSimulator{cfg: cfg, text: text, audio: audio}
}

// GetNextUserMessage returns the text simulator's next turn as speech. A turn
// that is not a success, or has no text, is returned unchanged.
func (s *AudioUserSimulator) GetNextUserMessage(ctx context.Context, events []*session.Event) (NextUserMessage, error) {
	next, err := s.text.GetNextUserMessage(ctx, events)
	if err != nil || next.Status != StatusSuccess || next.UserMessage == nil {
		return next, err
	}
	var text strings.Builder
	for _, p := range next.UserMessage.Parts {
		if p != nil {
			text.WriteString(p.Text)
		}
	}
	if text.Len() == 0 {
		return next, nil
	}
	content, err := s.toAudioContent(ctx, text.String())
	if err != nil {
		return NextUserMessage{}, err
	}
	return NextUserMessage{Status: StatusSuccess, UserMessage: content}, nil
}

// toAudioContent returns a user message speaking text, resampled for live
// input, after the text itself when the config keeps it.
func (s *AudioUserSimulator) toAudioContent(ctx context.Context, text string) (*genai.Content, error) {
	data, mime, err := s.generateAudio(ctx, text)
	if err != nil {
		return nil, err
	}
	live, err := ToLiveInput(data, mime)
	if err != nil {
		return nil, err
	}
	var parts []*genai.Part
	if s.cfg.IncludeTextWithAudio == nil || *s.cfg.IncludeTextWithAudio {
		parts = append(parts, &genai.Part{Text: text})
	}
	parts = append(parts, &genai.Part{InlineData: &genai.Blob{MIMEType: LiveInputMIMEType, Data: live}})
	return &genai.Content{Role: genai.RoleUser, Parts: parts}, nil
}

// generateAudio asks the audio model to speak text, joining the audio of
// every response; the last MIME type given wins, "audio/pcm" by default.
func (s *AudioUserSimulator) generateAudio(ctx context.Context, text string) ([]byte, string, error) {
	req := &model.LLMRequest{
		Model:    s.cfg.AudioModel,
		Config:   cloneAudioConfig(s.cfg.AudioModelConfiguration),
		Contents: []*genai.Content{genai.NewContentFromText(text, genai.RoleUser)},
	}
	var data []byte
	mime := "audio/pcm"
	for resp, err := range s.audio.GenerateContent(ctx, req, false) {
		if err != nil {
			return nil, "", fmt.Errorf("Audio generation failed: %w", err)
		}
		if resp == nil {
			continue
		}
		if resp.ErrorCode != "" {
			return nil, "", fmt.Errorf("Audio generation failed: %s — %s", resp.ErrorCode, resp.ErrorMessage)
		}
		if resp.Content == nil {
			continue
		}
		for _, p := range resp.Content.Parts {
			if p == nil || p.InlineData == nil || len(p.InlineData.Data) == 0 {
				continue
			}
			data = append(data, p.InlineData.Data...)
			if p.InlineData.MIMEType != "" {
				mime = p.InlineData.MIMEType
			}
		}
	}
	if len(data) == 0 {
		return nil, "", errors.New("Audio model returned no audio data")
	}
	return data, mime, nil
}

// cloneAudioConfig copies c for one request. Every case's simulator shares
// the run's config, and models write into the request's config (ADK's gemini
// model adds headers to HTTPOptions), so concurrent cases must not share it.
func cloneAudioConfig(c *genai.GenerateContentConfig) *genai.GenerateContentConfig {
	if c == nil {
		return nil
	}
	cp := *c
	if c.HTTPOptions != nil {
		opts := *c.HTTPOptions
		opts.Headers = c.HTTPOptions.Headers.Clone()
		cp.HTTPOptions = &opts
	}
	return &cp
}
