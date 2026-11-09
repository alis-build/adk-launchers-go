package tooloauth

import (
	"context"
	"fmt"
	"iter"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/auth"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/genai"
)

// scriptedModel calls one tool for every user message and answers "done"
// once the tool's response comes back, so each user turn runs the tool once.
type scriptedModel struct {
	toolName string
	mu       sync.Mutex
	calls    int
}

func (m *scriptedModel) Name() string { return "scripted" }

func (m *scriptedModel) GenerateContent(_ context.Context, req *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		var last *genai.Content
		if n := len(req.Contents); n > 0 {
			last = req.Contents[n-1]
		}
		if last != nil {
			for _, p := range last.Parts {
				if p != nil && p.FunctionResponse != nil {
					yield(&model.LLMResponse{Content: genai.NewContentFromText("done", genai.RoleModel)}, nil)
					return
				}
			}
		}
		m.mu.Lock()
		m.calls++
		id := fmt.Sprintf("call-%d", m.calls)
		m.mu.Unlock()
		yield(&model.LLMResponse{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{
			{FunctionCall: &genai.FunctionCall{ID: id, Name: m.toolName, Args: map[string]any{}}},
		}}}, nil)
	}
}

// resourceServer is the third-party API a tool calls; it records each
// request's Authorization header, and counts how often calendarTool's
// handler ran, whether or not its request got through.
type resourceServer struct {
	*httptest.Server
	mu       sync.Mutex
	auths    []string
	handlers int
}

func newResourceServer(t *testing.T) *resourceServer {
	t.Helper()
	rs := &resourceServer{}
	rs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rs.mu.Lock()
		rs.auths = append(rs.auths, r.Header.Get("Authorization"))
		rs.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(rs.Close)
	return rs
}

// handlerRuns returns how often calendarTool's handler ran.
func (rs *resourceServer) handlerRuns() int {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return rs.handlers
}

// authorizations returns the Authorization headers seen so far.
func (rs *resourceServer) authorizations() []string {
	rs.mu.Lock()
	defer rs.mu.Unlock()
	return append([]string(nil), rs.auths...)
}

// calendarTool returns the calendar_list tool, which GETs /events on rs
// through auth.Transport with the provider cfg hands out, and returns the
// status code.
func calendarTool(t *testing.T, cfg Config, rs *resourceServer) tool.Tool {
	t.Helper()
	p, err := cfg.Provider("calendar_list")
	if err != nil {
		t.Fatalf("Provider() error = %v", err)
	}
	client := &http.Client{Transport: &auth.Transport{Provider: p}}
	tl, err := functiontool.New(functiontool.Config{Name: "calendar_list", Description: "Lists calendar events."},
		func(ctx agent.Context, _ struct{}) (map[string]any, error) {
			rs.mu.Lock()
			rs.handlers++
			rs.mu.Unlock()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, rs.URL+"/events", nil)
			if err != nil {
				return nil, err
			}
			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()
			return map[string]any{"status": resp.StatusCode}, nil
		})
	if err != nil {
		t.Fatalf("functiontool.New() error = %v", err)
	}
	return tl
}

// harness runs one llmagent, scripted to call toolName, with the tooloauth
// plugin built from cfg.
type harness struct {
	t      *testing.T
	runner *runner.Runner
}

func newHarness(t *testing.T, appName, toolName string, cfg Config, tools ...tool.Tool) *harness {
	t.Helper()
	p, err := NewPlugin(cfg)
	if err != nil {
		t.Fatalf("NewPlugin() error = %v", err)
	}
	a, err := llmagent.New(llmagent.Config{Name: "assistant", Model: &scriptedModel{toolName: toolName}, Tools: tools})
	if err != nil {
		t.Fatalf("llmagent.New() error = %v", err)
	}
	r, err := runner.New(runner.Config{
		AppName:           appName,
		Agent:             a,
		SessionService:    session.InMemoryService(),
		PluginConfig:      runner.PluginConfig{Plugins: []*plugin.Plugin{p}},
		AutoCreateSession: true,
	})
	if err != nil {
		t.Fatalf("runner.New() error = %v", err)
	}
	return &harness{t: t, runner: r}
}

// run sends content as userID in sessionID and returns the run's events,
// failing the test on an event error. content is a user text message, or a
// FunctionResponse answering a confirmation request.
func (h *harness) run(ctx context.Context, userID, sessionID string, content *genai.Content) []*session.Event {
	h.t.Helper()
	var events []*session.Event
	for ev, err := range h.runner.Run(ctx, userID, sessionID, content, agent.RunConfig{}) {
		if err != nil {
			h.t.Fatalf("run event error: %v", err)
		}
		events = append(events, ev)
	}
	return events
}

// functionResponse returns the response of the first call to name in events.
func functionResponse(events []*session.Event, name string) (map[string]any, bool) {
	for _, ev := range events {
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p != nil && p.FunctionResponse != nil && p.FunctionResponse.Name == name {
				return p.FunctionResponse.Response, true
			}
		}
	}
	return nil, false
}

// hasFunctionCall reports whether any event calls name.
func hasFunctionCall(events []*session.Event, name string) bool {
	for _, ev := range events {
		if ev.Content == nil {
			continue
		}
		for _, p := range ev.Content.Parts {
			if p != nil && p.FunctionCall != nil && p.FunctionCall.Name == name {
				return true
			}
		}
	}
	return false
}
