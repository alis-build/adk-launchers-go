package generator

import (
	"strings"

	"go.alis.build/adk/launchers/evals/evaluation/models"
	"google.golang.org/adk/session"
	"google.golang.org/genai"
)

// GetAppDetailsByInvocationID builds AppDetails maps from intercepted model requests.
func GetAppDetailsByInvocationID(events []*session.Event, interceptor *RequestInterceptor) map[string]*models.AppDetails {
	if interceptor == nil {
		return nil
	}
	grouped := CollectEventsByInvocationID(events)
	out := make(map[string]*models.AppDetails, len(grouped))
	for invocationID, invEvents := range grouped {
		details := &models.AppDetails{AgentDetails: map[string]models.AgentDetails{}}
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
			var tools []*genai.Tool
			if llmRequest.Config != nil {
				tools = llmRequest.Config.Tools
			}
			details.AgentDetails[agentName] = models.AgentDetails{
				Name:             agentName,
				Instructions:     systemInstructionText(llmRequest.Config),
				ToolDeclarations: tools,
			}
		}
		if len(details.AgentDetails) == 0 {
			continue
		}
		out[invocationID] = details
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
