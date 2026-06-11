package orchestration

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/domain"
	"github.com/thejorgg/taskforce/internal/rescue"
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

func TestPipelineRescueRemapsTempRepoAndRestoresBeforeScope(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# TaskForce\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Relay.Control.Run = "echo planned"
	cfg.Relay.Control.Agent = ""
	cfg.Relay.Build.Run = "mimo would run here"
	cfg.Relay.Build.Agent = ""
	cfg.Rescue.Root = filepath.Join(t.TempDir(), "relay-rescue")
	cfg.Scope.Hooks = []config.HookConfig{{Name: "ok", Run: "echo ok", Required: true}}
	cfg.Exfil.Commit = false
	cfg.Exfil.Branch = ""
	var rescueRepo string
	r := runner.Runner{Options: runner.Options{
		Timeout: time.Minute,
		Yes:     true,
		Executor: func(_ context.Context, spec domain.CommandSpec) domain.CommandResult {
			result := domain.CommandResult{Name: spec.Name, Command: spec.Run, ExitCode: 0}
			if spec.Name == "relay.build" {
				result.ExitCode = 23
				result.Stderr = "refusal: security policy flagged the repository vocabulary"
				result.Error = "exit status 23"
				return result
			}
			if spec.Name == "relay.build.rescue" {
				rescueRepo = spec.WorkDir
				data, err := os.ReadFile(filepath.Join(spec.WorkDir, "README.md"))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(data), "TaskForce") || !strings.Contains(string(data), "TFNeutralProject") {
					t.Fatalf("rescue repo was not neutralized: %q", data)
				}
				next := strings.Replace(string(data), "\n", "\n\nfixed in rescue\n", 1)
				if err := os.WriteFile(filepath.Join(spec.WorkDir, "README.md"), []byte(next), 0o644); err != nil {
					t.Fatal(err)
				}
				result.Stdout = "rescued"
				return result
			}
			result.Stdout = "ok"
			return result
		},
	}}
	p := New(Options{Config: cfg, Repo: repo, Runner: r})
	run := p.RunText(context.Background(), "test", "Fix broken login")
	if run.Review.Status != domain.ReviewApproved {
		t.Fatalf("review status = %s, relay feedback %q", run.Review.Status, run.Relay.Feedback)
	}
	if !run.Relay.Approved {
		t.Fatalf("relay was not approved after rescue: %#v", run.Relay)
	}
	if rescueRepo == "" || !strings.Contains(rescueRepo, "relay-rescue") {
		t.Fatalf("rescue repo was not used: %q", rescueRepo)
	}
	data, err := os.ReadFile(filepath.Join(repo, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); !strings.Contains(got, "TaskForce") || strings.Contains(got, "TFNeutralProject") || !strings.Contains(got, "fixed in rescue") {
		t.Fatalf("restored README = %q", got)
	}
}

func TestPipelineUsesPersistedRescueMappingsBeforeFreshFailure(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# TaskForce\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	persisted := map[string]string{"TaskForce": "TFNeutralProject"}
	if err := rescue.StoreMappings(repo, persisted); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Relay.Control.Enabled = false
	cfg.Relay.Build.Run = "echo build"
	cfg.Relay.Build.Agent = ""
	cfg.Rescue.Root = filepath.Join(t.TempDir(), "relay-rescue")
	cfg.Scope.Hooks = []config.HookConfig{{Name: "ok", Run: "echo ok", Required: true}}
	cfg.Exfil.Commit = false
	cfg.Exfil.Branch = ""
	mappedBuild := false
	r := runner.Runner{Options: runner.Options{
		Timeout: time.Minute,
		Yes:     true,
		Executor: func(_ context.Context, spec domain.CommandSpec) domain.CommandResult {
			result := domain.CommandResult{Name: spec.Name, Command: spec.Run, ExitCode: 0, Stdout: "ok"}
			if spec.Name == "relay.build.mapped" {
				mappedBuild = true
				data, err := os.ReadFile(filepath.Join(spec.WorkDir, "README.md"))
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(data), "TaskForce") || !strings.Contains(string(data), "TFNeutralProject") {
					t.Fatalf("persisted mapping was not applied: %q", data)
				}
			}
			return result
		},
	}}
	run := New(Options{Config: cfg, Repo: repo, Runner: r}).RunText(context.Background(), "test", "Run with persisted rescue state")
	if !mappedBuild {
		t.Fatal("expected persisted rescue mappings to force mapped relay.build")
	}
	if run.Review.Status != domain.ReviewApproved {
		t.Fatalf("review status = %s", run.Review.Status)
	}
}
