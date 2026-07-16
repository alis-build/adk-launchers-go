package generator

import (
	"fmt"
	"sync"

	"github.com/google/uuid"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/plugin"
	"google.golang.org/adk/session"
)

const llmRequestIDKey = "__llm_request_key__"

// RequestInterceptor caches LLM requests and links them to model responses.
type RequestInterceptor struct {
	mu       sync.RWMutex
	requests map[string]*model.LLMRequest
	plugin   *plugin.Plugin
}

// NewRequestInterceptor builds the eval request interceptor plugin. Returns an
// error if plugin construction fails so callers can surface the failure
// instead of taking down the request goroutine with a panic.
func NewRequestInterceptor() (*RequestInterceptor, error) {
	r := &RequestInterceptor{requests: make(map[string]*model.LLMRequest)}
	p, err := plugin.New(plugin.Config{
		Name: "request_intercepter_plugin",
		BeforeModelCallback: func(ctx agent.CallbackContext, llmRequest *model.LLMRequest) (*model.LLMResponse, error) {
			requestID := uuid.NewString()
			r.mu.Lock()
			r.requests[requestID] = cloneLLMRequest(llmRequest)
			r.mu.Unlock()
			if err := ctx.State().Set(llmRequestIDKey, requestID); err != nil {
				return nil, err
			}
			return nil, nil
		},
		AfterModelCallback: func(ctx agent.CallbackContext, llmResponse *model.LLMResponse, _ error) (*model.LLMResponse, error) {
			val, err := ctx.State().Get(llmRequestIDKey)
			if err != nil || val == nil {
				return llmResponse, nil
			}
			requestID, ok := val.(string)
			if !ok || requestID == "" {
				return llmResponse, nil
			}
			if llmResponse.CustomMetadata == nil {
				llmResponse.CustomMetadata = map[string]any{}
			}
			llmResponse.CustomMetadata[llmRequestIDKey] = requestID
			return llmResponse, nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("build request interceptor plugin: %w", err)
	}
	r.plugin = p
	return r, nil
}

// Plugin returns the ADK plugin registered on the runner.
func (r *RequestInterceptor) Plugin() *plugin.Plugin {
	return r.plugin
}

// GetModelRequest returns the cached request for an event, if present.
func (r *RequestInterceptor) GetModelRequest(event *session.Event) *model.LLMRequest {
	if event == nil || event.CustomMetadata == nil {
		return nil
	}
	rawID, ok := event.CustomMetadata[llmRequestIDKey]
	if !ok {
		return nil
	}
	requestID, ok := rawID.(string)
	if !ok || requestID == "" {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.requests[requestID]
}

// cloneLLMRequest deep-copies config and tools so cached requests are immutable.
func cloneLLMRequest(req *model.LLMRequest) *model.LLMRequest {
	if req == nil {
		return nil
	}
	out := *req
	if req.Config != nil {
		cfg := *req.Config
		out.Config = &cfg
	}
	if req.Tools != nil {
		out.Tools = make(map[string]any, len(req.Tools))
		for k, v := range req.Tools {
			out.Tools[k] = v
		}
	}
	return &out
}
