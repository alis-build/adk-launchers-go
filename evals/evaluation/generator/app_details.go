package generator

import (
	"encoding/json"
	"strings"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// GetAppDetailsByInvocationID builds AppDetails maps from intercepted model requests.
func GetAppDetailsByInvocationID(events []*session.Event, interceptor *RequestInterceptor) map[string]json.RawMessage {
	if interceptor == nil {
		return nil
	}
	grouped := CollectEventsByInvocationID(events)
	out := make(map[string]json.RawMessage, len(grouped))
	for invocationID, invEvents := range grouped {
		details := models.AppDetails{AgentDetails: map[string]models.AgentDetails{}}
		for _, event := range invEvents {
			if event == nil || strings.EqualFold(event.Author, userAuthor) {
				continue
			}
			llmRequest := interceptor.GetModelRequest(event)
			if llmRequest == nil {
				continue
			}
			agentName := event.Author
			if agentName == "" {
				agentName = defaultAuthor
			}
			if _, ok := details.AgentDetails[agentName]; ok {
				continue
			}
			toolsJSON, _ := json.Marshal(llmRequest.Config.Tools)
			details.AgentDetails[agentName] = models.AgentDetails{
				Name:             agentName,
				Instructions:     systemInstructionText(llmRequest.Config),
				ToolDeclarations: toolsJSON,
			}
		}
		raw, err := json.Marshal(details)
		if err != nil {
			continue
		}
		out[invocationID] = raw
	}
	return out
}

// systemInstructionText extracts system instruction text from GenerateContentConfig.
func systemInstructionText(cfg *genai.GenerateContentConfig) string {
	if cfg == nil || cfg.SystemInstruction == nil {
		return ""
	}
	var parts []string
	for _, p := range cfg.SystemInstruction.Parts {
		if p != nil && p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}
