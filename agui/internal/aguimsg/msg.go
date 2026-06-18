// Package aguimsg converts inbound AG-UI messages into genai.Content for ADK runs.
package aguimsg

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"google.golang.org/genai"
)

// ConvertMultimodalInput converts a typed AG-UI InputContent (image, audio,
// video, document) to a [genai.Part]. The AG-UI spec nests payload under
// source.{type,value,mimeType}; older clients may still send flat Data/URL fields.
func ConvertMultimodalInput(ic types.InputContent) (*genai.Part, error) {
	if ic.Source != nil {
		switch ic.Source.Type {
		case types.InputContentSourceTypeData:
			dataBytes, err := base64.StdEncoding.DecodeString(ic.Source.Value)
			if err != nil {
				return nil, fmt.Errorf("failed to decode base64 source data: %w", err)
			}
			return &genai.Part{
				InlineData: &genai.Blob{
					Data:     dataBytes,
					MIMEType: ic.Source.MimeType,
				},
			}, nil
		case types.InputContentSourceTypeURL:
			return &genai.Part{
				FileData: &genai.FileData{
					FileURI:  ic.Source.Value,
					MIMEType: ic.Source.MimeType,
				},
			}, nil
		default:
			return nil, fmt.Errorf("unsupported source type %q", ic.Source.Type)
		}
	}

	if ic.Data != "" {
		dataBytes, err := base64.StdEncoding.DecodeString(ic.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to decode base64 data: %w", err)
		}
		return &genai.Part{
			InlineData: &genai.Blob{
				Data:     dataBytes,
				MIMEType: ic.MimeType,
			},
		}, nil
	}

	if ic.URL != "" {
		return &genai.Part{
			FileData: &genai.FileData{
				FileURI:  ic.URL,
				MIMEType: ic.MimeType,
			},
		}, nil
	}

	return nil, fmt.Errorf("no data, url, or source available")
}

// ContentToResponseMap normalises tool message content into the map[string]any
// format expected by genai.FunctionResponse.Response.
func ContentToResponseMap(content any) map[string]any {
	switch v := content.(type) {
	case map[string]any:
		return v
	case string:
		if v == "" {
			return map[string]any{}
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(v), &m); err == nil {
			return m
		}
		return map[string]any{"result": v}
	case nil:
		return map[string]any{}
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return map[string]any{"result": fmt.Sprintf("%v", v)}
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			return map[string]any{"result": string(b)}
		}
		return m
	}
}

// ExtractToolResultContent builds a genai.Content from tool-role messages in the
// AG-UI message history.
func ExtractToolResultContent(messages []types.Message) (*genai.Content, error) {
	toolNames := make(map[string]string)
	for _, m := range messages {
		if m.Role != types.RoleAssistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			toolNames[tc.ID] = tc.Function.Name
		}
	}

	trailing := TrailingToolMessages(messages)

	var parts []*genai.Part
	for _, m := range trailing {
		name := toolNames[m.ToolCallID]
		if name == "" {
			name = m.ToolCallID
		}

		response := ContentToResponseMap(m.Content)

		parts = append(parts, &genai.Part{
			FunctionResponse: &genai.FunctionResponse{
				Name:     name,
				ID:       m.ToolCallID,
				Response: response,
			},
		})
	}
	if len(parts) == 0 {
		return nil, nil
	}
	return genai.NewContentFromParts(parts, genai.RoleUser), nil
}

// IsToolResultSubmission reports whether the trailing messages in the
// transcript are tool-role results being submitted by the client.
func IsToolResultSubmission(messages []types.Message) bool {
	return len(TrailingToolMessages(messages)) > 0
}

// TrailingToolMessages returns the consecutive run of role "tool" messages at
// the end of the transcript.
func TrailingToolMessages(messages []types.Message) []types.Message {
	i := len(messages) - 1
	for i >= 0 && messages[i].Role == types.RoleTool && messages[i].ToolCallID != "" {
		i--
	}
	start := i + 1
	if start >= len(messages) {
		return nil
	}
	return messages[start:]
}

// ExtractLastUserMessage returns the latest user turn from an AG-UI message history.
func ExtractLastUserMessage(messages []types.Message) (*genai.Content, error) {
	for i := len(messages) - 1; i >= 0; i-- {
		message := messages[i]
		if message.Role != types.RoleUser {
			continue
		}

		if inputContents, ok := message.ContentInputContents(); ok && len(inputContents) > 0 {
			parts := make([]*genai.Part, len(inputContents))
			for j, inputContent := range inputContents {
				switch inputContent.Type {
				case types.InputContentTypeText:
					parts[j] = genai.NewPartFromText(inputContent.Text)
				case types.InputContentTypeBinary:
					dataBytes, err := base64.StdEncoding.DecodeString(inputContent.Data)
					if err != nil {
						return nil, fmt.Errorf("failed to decode base64 binary data: %w", err)
					}
					parts[j] = &genai.Part{
						InlineData: &genai.Blob{
							Data:        dataBytes,
							MIMEType:    inputContent.MimeType,
							DisplayName: inputContent.Filename,
						},
					}
				default:
					part, err := ConvertMultimodalInput(inputContent)
					if err != nil {
						return nil, fmt.Errorf("unsupported content type %q: %w", inputContent.Type, err)
					}
					parts[j] = part
				}
			}
			return genai.NewContentFromParts(parts, genai.RoleUser), nil
		}

		if contentStr, ok := message.ContentString(); ok && contentStr != "" {
			return genai.NewContentFromText(contentStr, genai.RoleUser), nil
		}

		return nil, fmt.Errorf("unsupported content type: %T", message.Content)
	}

	return nil, fmt.Errorf("no user message found in payload")
}
