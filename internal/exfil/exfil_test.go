package exfil

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/domain"
	"github.com/thejorgg/taskforce/internal/runner"
)

func TestReleaseCreatesBranchAndCommit(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "taskforce@example.test")
	runGit(t, dir, "config", "user.name", "TaskForce Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "initial")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\nworld\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.ExfilConfig{Branch: "taskforce/test", Commit: true, CommitMessage: "TaskForce: {{task_title}}"}
	r := Releaser{Config: cfg, Runner: runner.Runner{Options: runner.Options{Repo: dir, Timeout: time.Minute, Yes: true}}}
	task := domain.TaskPacket{ID: "task-1", Title: "Test task"}
	result := r.Release(context.Background(), task, domain.ReviewResult{Status: domain.ReviewApproved})
	if result.Skipped {
		t.Fatalf("release skipped: %+v", result.Results)
	}
	if result.Branch != "taskforce/test" {
		t.Fatalf("branch = %q", result.Branch)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
