package generator

import (
	"fmt"
	"math"
	"slices"
	"sync"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/plugin"
	"google.golang.org/adk/v2/tool"
)

// interval is a span of time on one clock, in seconds since the clock's
// zero.
type interval struct{ start, end float64 }

// busyIntervals is when one invocation was awaiting a model or inside a tool.
// Intervals are kept whole, not summed, because they overlap: tools run
// concurrently and sub-agents share the invocation ID. An ADK agent tool runs
// its sub-agent on a runner of its own, without this plugin, so the
// sub-agent's model time is not seen here and counts as tool time (adk-python
// attributes it to the model).
type busyIntervals struct {
	model, tool []interval
}

// modelSeconds is the time with at least one model call in flight.
func (b busyIntervals) modelSeconds() float64 { return unionSeconds(b.model) }

// toolSeconds is the time inside a tool not already spent awaiting a model,
// so a model call made during a tool counts once, as model time.
func (b busyIntervals) toolSeconds() float64 {
	return unionSeconds(slices.Concat(b.model, b.tool)) - unionSeconds(b.model)
}

// unionSeconds is how long the union of intervals covers: overlapping
// intervals count once (adk-python's _union_seconds).
func unionSeconds(intervals []interval) float64 {
	sorted := slices.Clone(intervals)
	slices.SortFunc(sorted, func(a, b interval) int {
		if a.start != b.start {
			if a.start < b.start {
				return -1
			}
			return 1
		}
		if a.end < b.end {
			return -1
		}
		if a.end > b.end {
			return 1
		}
		return 0
	})
	total := 0.0
	countedTo := math.Inf(-1)
	for _, iv := range sorted {
		if iv.end <= countedTo {
			continue
		}
		total += iv.end - math.Max(iv.start, countedTo)
		countedTo = iv.end
	}
	return total
}

// turnTimer records each invocation's model and tool intervals through a
// plugin, for the efficiency duration split (adk-python's
// collect_busy_intervals). It must run first: ADK stops a plugin chain at
// the first callback that returns a value, and an app plugin that rewrites
// a reply or result would otherwise hide the interval's end. Its callbacks
// always return nothing, so going first changes nothing else.
type turnTimer struct {
	now    func() time.Time
	origin time.Time

	mu        sync.Mutex
	byInv     map[string]*busyIntervals
	modelOpen map[modelKey]float64
	toolOpen  map[toolKey]toolStart

	plugin *plugin.Plugin
}

type modelKey struct{ invocation, agent, branch string }

// toolKey names one tool call: the function call ID within its invocation
// and branch, so even two calls a model gave the same ID are told apart.
type toolKey struct{ invocation, branch, call string }

// toolStart is an open tool run: its invocation and start.
type toolStart struct {
	invocation string
	at         float64
}

// newTurnTimer returns a timer reading now.
func newTurnTimer(now func() time.Time) (*turnTimer, error) {
	t := &turnTimer{
		now:       now,
		origin:    now(),
		byInv:     map[string]*busyIntervals{},
		modelOpen: map[modelKey]float64{},
		toolOpen:  map[toolKey]toolStart{},
	}
	p, err := plugin.New(plugin.Config{
		Name: "eval_turn_timer",
		BeforeModelCallback: func(ctx agent.Context, _ *model.LLMRequest) (*model.LLMResponse, error) {
			t.openModel(ctx)
			return nil, nil
		},
		AfterModelCallback: func(ctx agent.Context, _ *model.LLMResponse, _ error) (*model.LLMResponse, error) {
			// Each streamed chunk is one wait: close it and start the next.
			// The wait reopened after the last chunk is never closed: the
			// next model call replaces it, and the turn's end drops it, so
			// the tool run and the next request's preparation are not
			// billed as model time.
			t.closeModel(ctx, true)
			return nil, nil
		},
		OnModelErrorCallback: func(ctx agent.Context, _ *model.LLMRequest, _ error) (*model.LLMResponse, error) {
			// A failed call still spent model time.
			t.closeModel(ctx, false)
			return nil, nil
		},
		BeforeToolCallback: func(ctx agent.Context, _ tool.Tool, _ map[string]any) (map[string]any, error) {
			t.openTool(ctx)
			return nil, nil
		},
		// ADK calls AfterToolCallback for every run, a failed one included
		// (after OnToolErrorCallback), so the run is closed there, once.
		AfterToolCallback: func(ctx agent.Context, _ tool.Tool, _, _ map[string]any, _ error) (map[string]any, error) {
			t.closeTool(ctx)
			return nil, nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("build turn timer plugin: %w", err)
	}
	t.plugin = p
	return t, nil
}

// at is now as seconds since the timer's origin.
func (t *turnTimer) at() float64 { return t.now().Sub(t.origin).Seconds() }

// intervals returns invocation's record, creating it.
func (t *turnTimer) intervals(invocation string) *busyIntervals {
	b := t.byInv[invocation]
	if b == nil {
		b = &busyIntervals{}
		t.byInv[invocation] = b
	}
	return b
}

func (t *turnTimer) openModel(ctx agent.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.modelOpen[modelKey{ctx.InvocationID(), ctx.AgentName(), ctx.Branch()}] = t.at()
}

// closeModel closes the open model wait for ctx, and reopens one when
// reopen is set.
func (t *turnTimer) closeModel(ctx agent.Context, reopen bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := modelKey{ctx.InvocationID(), ctx.AgentName(), ctx.Branch()}
	start, ok := t.modelOpen[k]
	now := t.at()
	if ok {
		b := t.intervals(k.invocation)
		b.model = append(b.model, interval{start, now})
	}
	if reopen {
		t.modelOpen[k] = now
	} else {
		delete(t.modelOpen, k)
	}
}

// openTool starts timing the tool call ctx is for, keyed by its function
// call ID, so concurrent calls of one tool are told apart.
func (t *turnTimer) openTool(ctx agent.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.toolOpen[toolKey{ctx.InvocationID(), ctx.Branch(), ctx.FunctionCallID()}] = toolStart{invocation: ctx.InvocationID(), at: t.at()}
}

// closeTool closes the tool call ctx is for; closing it twice is harmless.
func (t *turnTimer) closeTool(ctx agent.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()
	k := toolKey{ctx.InvocationID(), ctx.Branch(), ctx.FunctionCallID()}
	start, ok := t.toolOpen[k]
	if !ok {
		return
	}
	delete(t.toolOpen, k)
	b := t.intervals(start.invocation)
	b.tool = append(b.tool, interval{start.at, t.at()})
}

// busy returns invocation's recorded intervals.
func (t *turnTimer) busy(invocation string) busyIntervals {
	t.mu.Lock()
	defer t.mu.Unlock()
	if b := t.byInv[invocation]; b != nil {
		return busyIntervals{model: slices.Clone(b.model), tool: slices.Clone(b.tool)}
	}
	return busyIntervals{}
}
