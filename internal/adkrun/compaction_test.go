package adkrun

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"sync"
	"testing"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/cmd/launcher"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/session/compaction"
	"google.golang.org/genai"
)

var (
	errSummarizerUnavailable = errors.New("summarizer unavailable")
	errAgentFailed           = errors.New("agent failed")
)

// countingSummarizer records that compaction reached the summarizer, and can be
// made to fail.
type countingSummarizer struct {
	mu    sync.Mutex
	calls int
	err   error
}

func (c *countingSummarizer) SummarizeEvents(context.Context, []*session.Event) (compaction.SummarizeResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.err != nil {
		return compaction.SummarizeResult{}, c.err
	}
	return compaction.SummarizeResult{Content: genai.NewContentFromText("a summary", genai.RoleModel)}, nil
}

func (c *countingSummarizer) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.calls
}

// compactionTestRuntime returns a Runtime whose agent answers with one model
// event, so each turn leaves a complete exchange for a window to cover.
//
// The agent is a plain custom agent rather than an llmagent because cfg names a
// Summarizer explicitly; the runner only needs an LLM agent when it has to
// build the default summarizer over that agent's model.
func compactionTestRuntime(t *testing.T, cfg *compaction.Config) *Runtime {
	t.Helper()

	const appName = "compaction-agent"
	a, err := agent.New(agent.Config{
		Name: appName,
		Run: func(ictx agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				ev := session.NewEvent(ictx, ictx.InvocationID())
				ev.Author = appName
				ev.Content = genai.NewContentFromText("an answer", genai.RoleModel)
				yield(ev, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	rt, err := NewRuntime(&launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: session.InMemoryService(),
		Compaction:     cfg,
	}, appName)
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}
	return rt
}

// runTurns drives n turns on one session and returns every event the iterator
// yielded, failing the test on any streamed error.
func runTurns(t *testing.T, rt *Runtime, n int) []*Event {
	t.Helper()

	var got []*Event
	sessionID := ""
	for range n {
		id, events, err := rt.RunSSE(t.Context(), RunRequest{
			UserID:     "user-1",
			SessionID:  sessionID,
			NewMessage: UserTextMessage("hello"),
		})
		if err != nil {
			t.Fatalf("RunSSE: %v", err)
		}
		sessionID = id
		for ev, evErr := range events {
			if evErr != nil {
				t.Fatalf("event error: %v", evErr)
			}
			got = append(got, ev)
		}
	}
	return got
}

// TestRunSSEActuallyRunsCompaction pins that the compaction config on
// [launcher.Config] reaches the runner this package builds per request.
//
// Counting summarizer calls is what distinguishes "compaction ran" from "this
// surface accepts a compaction config and does nothing with it". Every other
// test in this package leaves Compaction nil, so dropping the field from the
// runner.Config literal keeps them all green.
func TestRunSSEActuallyRunsCompaction(t *testing.T) {
	summarizer := &countingSummarizer{}
	rt := compactionTestRuntime(t, &compaction.Config{CompactionInterval: 1, Summarizer: summarizer})

	runTurns(t, rt, 2)

	if summarizer.count() == 0 {
		t.Error("the summarizer was never called, so adkrun accepts a compaction config and does nothing with it")
	}
}

// TestRunSSEDoesNotYieldTheCompactionSummary pins the ADK contract the AG-UI
// launcher depends on: the summary is bookkeeping for the next prompt, not part
// of the conversation, so it never reaches the caller's iterator.
//
// A summary event is authored "user" and carries no content of its own. Were it
// streamed, the AG-UI processor would read the author change and emit a step for
// a turn that no one took.
func TestRunSSEDoesNotYieldTheCompactionSummary(t *testing.T) {
	summarizer := &countingSummarizer{}
	rt := compactionTestRuntime(t, &compaction.Config{CompactionInterval: 1, Summarizer: summarizer})

	got := runTurns(t, rt, 2)

	if summarizer.count() == 0 {
		t.Fatal("the summarizer was never called, so this test is not exercising compaction")
	}
	for _, ev := range got {
		if ev != nil && ev.Actions.Compaction != nil {
			t.Error("a compaction summary was streamed to the caller, which the AG-UI stream would render as a turn")
		}
	}
}

// TestRunSSESurvivesACompactionFailure pins that a failed summary neither costs
// the caller the answer the agent already produced nor reaches them as a
// stream error.
//
// Compaction runs after the invocation is over and after its events are stored,
// so a failure there means only that a later prompt will be larger. Every
// consumer of this iterator treats a streamed error as terminal: the AG-UI
// executor emits RunError instead of RunFinished, and the scheduler records the
// cron tick as failed. Passing the failure on would therefore turn a delivered
// answer into a failed run, so it is logged here and dropped, which is the
// handling [compaction.ErrCompaction] exists to allow.
func TestRunSSESurvivesACompactionFailure(t *testing.T) {
	summarizer := &countingSummarizer{err: errSummarizerUnavailable}
	rt := compactionTestRuntime(t, &compaction.Config{CompactionInterval: 1, Summarizer: summarizer})

	var answered bool
	var streamErr error
	sessionID := ""
	for range 2 {
		id, events, err := rt.RunSSE(t.Context(), RunRequest{
			UserID:     "user-1",
			SessionID:  sessionID,
			NewMessage: UserTextMessage("hello"),
		})
		if err != nil {
			t.Fatalf("RunSSE: %v", err)
		}
		sessionID = id
		for ev, evErr := range events {
			if evErr != nil {
				streamErr = evErr
				continue
			}
			if ev != nil && ev.Content != nil {
				answered = true
			}
		}
	}

	if summarizer.count() == 0 {
		t.Fatal("the summarizer was never called, so this test is not exercising the failure path")
	}
	if !answered {
		t.Error("a compaction failure cost the caller the agent's answer")
	}
	if streamErr != nil {
		t.Errorf("a compaction failure reached the caller as a stream error, which every consumer treats as a failed run: %v", streamErr)
	}
}

// TestRunSSEStillReportsRealFailures pins that dropping compaction errors does
// not also drop the errors that mean the turn itself failed.
func TestRunSSEStillReportsRealFailures(t *testing.T) {
	a, err := agent.New(agent.Config{
		Name: "err-agent",
		Run: func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {
				yield(nil, errAgentFailed)
			}
		},
	})
	if err != nil {
		t.Fatalf("create agent: %v", err)
	}

	rt, err := NewRuntime(&launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: session.InMemoryService(),
		Compaction:     &compaction.Config{CompactionInterval: 1, Summarizer: &countingSummarizer{}},
	}, "err-agent")
	if err != nil {
		t.Fatalf("NewRuntime: %v", err)
	}

	_, events, err := rt.RunSSE(t.Context(), RunRequest{
		UserID:     "user-1",
		NewMessage: UserTextMessage("hello"),
	})
	if err != nil {
		t.Fatalf("RunSSE: %v", err)
	}

	var seenErr error
	for _, evErr := range events {
		if evErr != nil {
			seenErr = evErr
			break
		}
	}
	if seenErr == nil {
		t.Fatal("an agent failure was swallowed along with compaction failures")
	}
}

// TestWithoutCompactionErrors covers the filter directly, including the early
// exit, which the runner-driven tests above cannot reach: callers that build
// their own runner (eval live inference) rely on this function rather than on
// RunSSE applying it.
func TestWithoutCompactionErrors(t *testing.T) {
	t.Parallel()

	compactionErr := fmt.Errorf("%w: post-invocation: %w", compaction.ErrCompaction, errSummarizerUnavailable)
	first, second := &Event{ID: "first"}, &Event{ID: "second"}

	t.Run("drops compaction errors and keeps events", func(t *testing.T) {
		src := func(yield func(*Event, error) bool) {
			yield(first, nil)
			yield(nil, compactionErr)
			yield(second, nil)
		}

		var ids []string
		for ev, err := range WithoutCompactionErrors(t.Context(), "test", src) {
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			ids = append(ids, ev.ID)
		}
		if len(ids) != 2 || ids[0] != "first" || ids[1] != "second" {
			t.Fatalf("events = %v, want [first second]", ids)
		}
	})

	t.Run("passes other errors through", func(t *testing.T) {
		src := func(yield func(*Event, error) bool) {
			yield(nil, errAgentFailed)
		}

		var got error
		for _, err := range WithoutCompactionErrors(t.Context(), "test", src) {
			got = err
		}
		if !errors.Is(got, errAgentFailed) {
			t.Fatalf("err = %v, want %v", got, errAgentFailed)
		}
	})

	t.Run("stops when the consumer breaks", func(t *testing.T) {
		delivered := 0
		src := func(yield func(*Event, error) bool) {
			for range 3 {
				if !yield(first, nil) {
					return
				}
				delivered++
			}
		}

		for range WithoutCompactionErrors(t.Context(), "test", src) {
			break
		}
		if delivered != 0 {
			t.Fatalf("source produced %d events after the consumer stopped, want 0", delivered)
		}
	})
}

// TestNewRuntimeAcceptsNilCompaction pins that leaving compaction unset stays
// valid, and that a run still works.
//
// NewRuntime validates through a nil receiver, which reads like an oversight and
// invites a nil guard. A guard that rejects nil would break every application
// that does not ask for compaction; other tests in this package leave Compaction
// nil, so it would fail them too, but not with a reason that names the cause.
func TestNewRuntimeAcceptsNilCompaction(t *testing.T) {
	t.Parallel()

	a, _ := agent.New(agent.Config{Name: "app", Run: func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {}
	}})

	rt, err := NewRuntime(&launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: session.InMemoryService(),
		Compaction:     nil,
	}, "app")
	if err != nil {
		t.Fatalf("NewRuntime rejected a nil Compaction, which means compaction disabled: %v", err)
	}

	if _, _, err := rt.RunSSE(t.Context(), RunRequest{
		UserID:     "user-1",
		NewMessage: UserTextMessage("hello"),
	}); err != nil {
		t.Fatalf("RunSSE with compaction disabled: %v", err)
	}
}

// TestNewRuntimeRejectsAnUnusableCompactionConfig pins that a config that can
// never compact is reported when the runtime is built.
//
// The runner is built per request and validates compaction there, so without a
// check here an unusable setting produces a process that starts cleanly and
// then fails every request.
func TestNewRuntimeRejectsAnUnusableCompactionConfig(t *testing.T) {
	t.Parallel()

	a, _ := agent.New(agent.Config{Name: "app", Run: func(agent.InvocationContext) iter.Seq2[*session.Event, error] {
		return func(yield func(*session.Event, error) bool) {}
	}})

	// OverlapSize without CompactionInterval: the sliding window never runs, so
	// the overlap describes a compaction that cannot happen.
	_, err := NewRuntime(&launcher.Config{
		AgentLoader:    agent.NewSingleLoader(a),
		SessionService: session.InMemoryService(),
		Compaction:     &compaction.Config{OverlapSize: 1},
	}, "app")
	if err == nil {
		t.Fatal("NewRuntime accepted a compaction config that can never compact")
	}
}
