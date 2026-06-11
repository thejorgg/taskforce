package rescue

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/domain"
	"github.com/thejorgg/taskforce/internal/harness"
	"github.com/thejorgg/taskforce/internal/runner"
)

func TestPrepareRejectsReplacementCollisions(t *testing.T) {
	repo := t.TempDir()
	mappings := map[string]string{
		"TaskForce": "TFNeutralProject",
	}
	materialized := materializeMappings(mappings)
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte(materialized["TaskForce"]+" already exists\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	team := Team{
		Config: config.RescueConfig{
			Enabled:  true,
			Agent:    "codex",
			Root:     filepath.Join(t.TempDir(), "rescue"),
			Mappings: mappings,
		},
		Repo: repo,
	}
	_, _, err := team.prepare(mappings)
	if err == nil {
		t.Fatal("expected replacement collision")
	}
	if !strings.Contains(err.Error(), "mapping collision") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTriggeredMatchesConfiguredRefusalText(t *testing.T) {
	result := domain.CommandResult{ExitCode: 17, Stderr: "I refuse because the security policy flagged this"}
	if !Triggered(config.RescueConfig{Enabled: true, Triggers: []string{"security policy"}}, &result) {
		t.Fatal("expected rescue trigger")
	}
	if Triggered(config.RescueConfig{Enabled: true, Triggers: []string{"security policy"}}, nil) {
		t.Fatal("nil result must not trigger")
	}
	result.ExitCode = 0
	if Triggered(config.RescueConfig{Enabled: true, Triggers: []string{"security policy"}}, &result) {
		t.Fatal("successful command must not trigger rescue")
	}
}

func TestRunRetriesUntilRescueSucceeds(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("# TaskForce\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	attempts := 0
	team := Team{
		Config: config.RescueConfig{
			Enabled:     true,
			Agent:       "rescue-shell",
			Root:        filepath.Join(t.TempDir(), "rescue"),
			MaxAttempts: 3,
			Mappings: map[string]string{
				"TaskForce": "TFNeutralProject",
			},
		},
		Agents: map[string]config.AgentConfig{
			"rescue-shell": {Run: "rescue"},
		},
		Repo: repo,
		Runner: runner.Runner{Options: runner.Options{
			Yes: true,
			Executor: func(_ context.Context, spec domain.CommandSpec) domain.CommandResult {
				attempts++
				result := domain.CommandResult{Name: spec.Name, Command: spec.Run, ExitCode: 19, Stderr: "still blocked"}
				if attempts == 2 {
					result.ExitCode = 0
					result.Stderr = ""
					result.Stdout = "ok"
					data, err := os.ReadFile(filepath.Join(spec.WorkDir, "README.md"))
					if err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(spec.WorkDir, "README.md"), append(data, []byte("\nrescued\n")...), 0o644); err != nil {
						t.Fatal(err)
					}
				}
				return result
			},
		}},
	}
	result, err := team.Run(context.Background(), "relay.build", harness.ModeBuild, domain.TaskPacket{ID: "t"}, "prompt")
	if err != nil {
		t.Fatal(err)
	}
	if result.Attempts != 2 || attempts != 2 {
		t.Fatalf("attempts = result %d executor %d, want 2", result.Attempts, attempts)
	}
	data, err := os.ReadFile(filepath.Join(repo, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); !strings.Contains(got, "TaskForce") || strings.Contains(got, "TFNeutralProject") || !strings.Contains(got, "rescued") {
		t.Fatalf("restored README = %q", got)
	}
}
