package merge

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/thejorgg/taskforce/internal/worktree"
)

func TestMergeAllNoWorktrees(t *testing.T) {
	dir := t.TempDir()
	_, err := MergeAll(dir, "main")
	if err == nil {
		t.Fatal("expected error for no worktrees")
	}
}

func TestMergeAllSuccessfulMerge(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")

	createFile(t, dir, "README.md", "initial")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial commit")

	runGit(t, dir, "checkout", "-b", "feature")
	createFile(t, dir, "feature.txt", "feature work")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "feature commit")
	runGit(t, dir, "checkout", "main")

	wtDir := filepath.Join(t.TempDir(), "wt1")
	cmd := exec.Command("git", "worktree", "add", wtDir, "feature")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}

	reg := &worktree.Registry{
		Worktrees: []worktree.Worktree{
			{Branch: "feature", Path: wtDir},
		},
	}
	if err := worktree.SaveRegistry(reg); err != nil {
		t.Fatal(err)
	}

	results, err := MergeAll(dir, "main")
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Conflict {
		t.Fatal("expected no conflict")
	}
}

func TestCheckMergeWarning(t *testing.T) {
	warning := CheckMergeWarning("feature", []string{"main", "feature"})
	if warning == nil {
		t.Fatal("expected warning")
	}
	if warning.Branch != "feature" {
		t.Fatalf("expected branch feature, got %s", warning.Branch)
	}

	noWarning := CheckMergeWarning("other", []string{"main", "feature"})
	if noWarning != nil {
		t.Fatal("expected no warning")
	}
}

func TestHandleConflictDefault(t *testing.T) {
	action := HandleConflict("test-branch")
	if action != ActionSkip {
		t.Fatalf("expected ActionSkip for invalid input, got %d", action)
	}
}

func createFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content+"\n"), 0o644); err != nil {
		t.Fatal(err)
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
