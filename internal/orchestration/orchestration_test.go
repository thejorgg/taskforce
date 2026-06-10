package orchestration

import (
	"context"
	"testing"
	"time"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/domain"
	"github.com/thejorgg/taskforce/internal/runner"
)

func TestPipelineHappyPath(t *testing.T) {
	cfg := config.Default()
	cfg.Relay.Control.Run = "echo planned"
	cfg.Relay.Control.Agent = ""
	cfg.Relay.Build.Run = "echo built"
	cfg.Relay.Build.Agent = ""
	cfg.Scope.Hooks = []config.HookConfig{{Name: "ok", Run: "echo ok", Required: true}}
	cfg.Exfil.Commit = false
	cfg.Exfil.Branch = ""
	r := runner.Runner{Options: runner.Options{Timeout: time.Minute, Yes: true}}
	p := New(Options{Config: cfg, Repo: t.TempDir(), Runner: r})
	run := p.RunText(context.Background(), "test", "Fix broken login")
	if run.Review.Status != domain.ReviewApproved {
		t.Fatalf("review status = %s", run.Review.Status)
	}
	if len(run.Stages) != 5 {
		t.Fatalf("stages = %d", len(run.Stages))
	}
}
