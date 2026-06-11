package merge

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thejorgg/taskforce/internal/worktree"
)

func TestMergeAllNoWorktrees(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")
	createFile(t, dir, "README.md", "init")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "init")

	_, _, err := MergeAll(dir, "main")
	if err == nil {
		t.Fatal("expected error for no worktrees")
	}
}

func TestMergeAllNotAGitRepo(t *testing.T) {
	dir := t.TempDir()
	_, _, err := MergeAll(dir, "main")
	if err == nil {
		t.Fatal("expected error for non-git repo")
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
	createFile(t, dir, "main.txt", "main work")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "main commit")

	wtDir := filepath.Join(t.TempDir(), "wt1")
	cmd := exec.Command("git", "worktree", "add", wtDir, "feature")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}

	reg := &worktree.Registry{
		Worktrees: []worktree.Worktree{
			{Branch: "feature", Path: wtDir, Parent: dir},
		},
	}
	if err := worktree.SaveRegistry(reg); err != nil {
		t.Fatal(err)
	}

	results, merged, err := MergeAll(dir, "main")
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Conflict {
		t.Fatal("expected no conflict")
	}
	if len(merged) != 1 || merged[0] != "feature" {
		t.Fatalf("expected [feature] in merged branches, got %v", merged)
	}

	out := runGitOutput(t, dir, "log", "--oneline")
	if !containsSubstring(out, "Merge") {
		t.Fatalf("expected a merge commit, got %q", out)
	}
}

func TestMergeAllSkipsTargetBranch(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")

	createFile(t, dir, "README.md", "initial")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial commit")

	runGit(t, dir, "checkout", "-b", "feature-b")
	createFile(t, dir, "b.txt", "b work")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "feature-b commit")
	runGit(t, dir, "checkout", "main")

	wtDir := filepath.Join(t.TempDir(), "wt-main")
	cmd := exec.Command("git", "worktree", "add", wtDir, "feature-b")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}

	reg := &worktree.Registry{
		Worktrees: []worktree.Worktree{
			{Branch: "feature-b", Path: wtDir, Parent: dir},
		},
	}
	if err := worktree.SaveRegistry(reg); err != nil {
		t.Fatal(err)
	}

	_, merged, err := MergeAll(dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	if len(merged) != 1 {
		t.Fatalf("expected 1 merged branch, got %v", merged)
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

func TestRecentMergesRoundTrip(t *testing.T) {
	branches := []string{"feat-a", "feat-b"}
	if err := StoreRecentMerges(branches); err != nil {
		t.Fatal(err)
	}
	loaded := LoadRecentMerges()
	if len(loaded) != 2 {
		t.Fatalf("expected 2 branches, got %d", len(loaded))
	}
	if loaded[0] != "feat-a" || loaded[1] != "feat-b" {
		t.Fatalf("expected [feat-a feat-b], got %v", loaded)
	}
	os.Remove(filepath.Join(storeDir(), "recent-merges.txt"))
}

func TestLoadRecentMergesEmpty(t *testing.T) {
	path := filepath.Join(storeDir(), "recent-merges.txt")
	os.Remove(path)
	loaded := LoadRecentMerges()
	if loaded != nil {
		t.Fatalf("expected nil, got %v", loaded)
	}
}

func TestCommitMerge(t *testing.T) {
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
	createFile(t, dir, "main.txt", "main work")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "main commit")

	cmd := exec.Command("git", "merge", "feature", "--no-ff", "--no-commit")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git merge: %v\n%s", err, out)
	}

	if err := commitMerge(dir, "feature", "main"); err != nil {
		t.Fatal(err)
	}

	out := runGitOutput(t, dir, "log", "-1", "--format=%s")
	if strings.TrimSpace(out) != "Merge feature into main" {
		t.Fatalf("expected commit message 'Merge feature into main', got %q", out)
	}
}

func TestAbortMerge(t *testing.T) {
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test")

	createFile(t, dir, "README.md", "initial\nmain\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "initial commit")

	runGit(t, dir, "checkout", "-b", "feature")
	createFile(t, dir, "README.md", "initial\nfeature\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "feature commit")

	runGit(t, dir, "checkout", "main")
	createFile(t, dir, "README.md", "initial\nconflict\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "main commit")

	cmd := exec.Command("git", "merge", "feature", "--no-ff")
	cmd.Dir = dir
	cmd.CombinedOutput()

	if err := abortMerge(dir); err != nil {
		t.Fatal(err)
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

func runGitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return string(os.ExpandEnv(string(out)))
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && contains(s, sub))
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
