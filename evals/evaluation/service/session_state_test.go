package service

import (
	"testing"

	"go.alis.build/adk/launchers/evals/evaluation/models"
)

func TestMergeSessionState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		runLevel  map[string]any
		caseLevel map[string]any
		want      map[string]any
	}{
		{
			name: "both empty",
		},
		{
			name:     "run only",
			runLevel: map[string]any{"idea_name": "ideas/run"},
			want:     map[string]any{"idea_name": "ideas/run"},
		},
		{
			name:      "case only",
			caseLevel: map[string]any{"idea_name": "ideas/case"},
			want:      map[string]any{"idea_name": "ideas/case"},
		},
		{
			name:      "case wins on conflict",
			runLevel:  map[string]any{"idea_name": "ideas/run", "account_name": "accounts/run"},
			caseLevel: map[string]any{"idea_name": "ideas/case"},
			want: map[string]any{
				"idea_name":    "ideas/case",
				"account_name": "accounts/run",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := mergeSessionState(tc.runLevel, tc.caseLevel)
			if len(tc.want) == 0 {
				if got != nil {
					t.Fatalf("got = %#v, want nil", got)
				}
				return
			}
			for k, want := range tc.want {
				if got[k] != want {
					t.Fatalf("%s = %v, want %v (full got = %#v)", k, got[k], want, got)
				}
			}
			if len(got) != len(tc.want) {
				t.Fatalf("len(got) = %d, want %d (got = %#v)", len(got), len(tc.want), got)
			}
		})
	}
}

func TestEffectiveSessionInput(t *testing.T) {
	t.Parallel()

	runLevel := map[string]any{
		"idea_name":    "ideas/run",
		"account_name": "accounts/run",
	}

	t.Run("no run state returns original pointer", func(t *testing.T) {
		input := &models.SessionInput{State: map[string]any{"idea_name": "ideas/case"}}
		evalCase := models.EvalCase{EvalID: "c1", SessionInput: input}
		got := effectiveSessionInput(nil, evalCase)
		if got != input {
			t.Fatalf("got pointer %p, want %p", got, input)
		}
	})

	t.Run("run only creates session input", func(t *testing.T) {
		got := effectiveSessionInput(runLevel, models.EvalCase{EvalID: "c1"})
		if got == nil || got.State["idea_name"] != "ideas/run" {
			t.Fatalf("got = %#v", got)
		}
	})

	t.Run("case overrides run", func(t *testing.T) {
		evalCase := models.EvalCase{
			EvalID: "c1",
			SessionInput: &models.SessionInput{
				AppName: "app",
				State:   map[string]any{"idea_name": "ideas/case"},
			},
		}
		got := effectiveSessionInput(runLevel, evalCase)
		if got.AppName != "app" {
			t.Fatalf("AppName = %q", got.AppName)
		}
		if got.State["idea_name"] != "ideas/case" {
			t.Fatalf("idea_name = %v", got.State["idea_name"])
		}
		if got.State["account_name"] != "accounts/run" {
			t.Fatalf("account_name = %v", got.State["account_name"])
		}
	})

	t.Run("empty run map treated as absent", func(t *testing.T) {
		input := &models.SessionInput{State: map[string]any{"idea_name": "ideas/case"}}
		evalCase := models.EvalCase{EvalID: "c1", SessionInput: input}
		got := effectiveSessionInput(map[string]any{}, evalCase)
		if got != input {
			t.Fatalf("got pointer %p, want %p", got, input)
		}
	})
}
