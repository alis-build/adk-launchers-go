package scheduler

import (
	"context"
	"testing"

	pb "go.alis.build/common/alis/agui/scheduler"

	"go.alis.build/adk/launchers/internal/adkrun"
	"go.alis.build/adk/launchers/tooloauth"
)

// interactivityRunner records whether each cron run's context was marked
// non-interactive.
type interactivityRunner struct {
	nonInteractive []bool
}

func (r *interactivityRunner) RunCron(ctx context.Context, _ adkrun.RunRequest) (string, error) {
	r.nonInteractive = append(r.nonInteractive, tooloauth.IsNonInteractive(ctx))
	return "sess-headless", nil
}

// Nobody can answer a sign-in pause during a cron run, so tools needing
// OAuth must fail with "connect <Provider> in chat first" instead.
func TestExecuteCron_runIsNonInteractive(t *testing.T) {
	svc := &fakeScheduler{
		cron: &pb.Cron{
			Name:   "crons/headless-1",
			Owner:  "users/alice",
			Email:  "alice@example.com",
			Prompt: "daily check-in",
			Type:   pb.Cron_TYPE_CRON,
		},
	}
	runner := &interactivityRunner{}

	if err := executeCron(context.Background(), svc, runner, testCronConfig(), svc.cron, "alice"); err != nil {
		t.Fatalf("executeCron: %v", err)
	}

	if len(runner.nonInteractive) != 1 || !runner.nonInteractive[0] {
		t.Errorf("runs marked non-interactive = %v, want [true]", runner.nonInteractive)
	}
}
