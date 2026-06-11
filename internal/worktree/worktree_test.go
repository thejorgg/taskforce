package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %s\n%s", err, out)
	}

	cmd = exec.Command("git", "config", "user.email", "test@test.com")
	cmd.Dir = dir
	cmd.Run()

	cmd = exec.Command("git", "config", "user.name", "Test")
	cmd.Dir = dir
	cmd.Run()

	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("initial"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd = exec.Command("git", "add", "file.txt")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add: %s\n%s", err, out)
	}

	cmd = exec.Command("git", "commit", "-m", "initial commit")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit: %s\n%s", err, out)
	}

	return dir
}

func createBranch(t *testing.T, repo, branch string) {
	t.Helper()
	cmd := exec.Command("git", "branch", branch)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git branch %s: %s\n%s", branch, err, out)
	}
}

func TestWorktreeStruct(t *testing.T) {
	wt := Worktree{
		Branch:  "feature-x",
		Path:    "/home/user/worktrees/feature-x",
		AddedAt: time.Now(),
		Parent:  "/home/user/repo",
	}

	if wt.Branch != "feature-x" {
		t.Errorf("expected branch feature-x, got %s", wt.Branch)
	}

	if wt.Parent != "/home/user/repo" {
		t.Errorf("expected parent /home/user/repo, got %s", wt.Parent)
	}
}

func TestFormatListEmpty(t *testing.T) {
	result := FormatList([]Worktree{})
	if result != "No tracked worktrees." {
		t.Errorf("expected 'No tracked worktrees.', got %q", result)
	}
}

func TestFormatListNonEmpty(t *testing.T) {
	wts := []Worktree{
		{Branch: "feat-a", Path: "/path/a", AddedAt: time.Date(2025, 1, 15, 10, 30, 0, 0, time.UTC), Parent: "/repo"},
	}
	result := FormatList(wts)
	if result == "" {
		t.Error("expected non-empty formatted list")
	}
}

func TestAddWorktree(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git worktree test in short mode")
	}

	repo := setupTestRepo(t)
	createBranch(t, repo, "test-branch")

	err := Add(repo, "test-branch")
	if err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	wtPath := GitWorktreePath("test-branch")
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Fatal("worktree directory was not created")
	}

	reg, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, wt := range reg.Worktrees {
		if wt.Branch == "test-branch" {
			found = true
			if wt.Parent != repo {
				t.Errorf("expected parent %s, got %s", repo, wt.Parent)
			}
			break
		}
	}
	if !found {
		t.Fatal("test-branch not found in registry after Add")
	}

	_ = Remove(repo, "test-branch")
}

func TestListWorktrees(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git worktree test in short mode")
	}

	repo := setupTestRepo(t)
	createBranch(t, repo, "list-branch")

	_ = Add(repo, "list-branch")

	wts, err := List(repo)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	found := false
	for _, wt := range wts {
		if wt.Branch == "list-branch" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("list-branch not found in List result")
	}

	_ = Remove(repo, "list-branch")
}

func TestRemoveWorktree(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git worktree test in short mode")
	}

	repo := setupTestRepo(t)
	createBranch(t, repo, "remove-branch")

	_ = Add(repo, "remove-branch")

	err := Remove(repo, "remove-branch")
	if err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	wtPath := GitWorktreePath("remove-branch")
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree directory still exists after Remove")
	}

	reg, err := LoadRegistry()
	if err != nil {
		t.Fatal(err)
	}

	for _, wt := range reg.Worktrees {
		if wt.Branch == "remove-branch" {
			t.Error("remove-branch still found in registry after Remove")
		}
	}
}

func TestRemoveNonExistent(t *testing.T) {
	repo := setupTestRepo(t)
	err := Remove(repo, "nonexistent-branch")
	if err == nil {
		t.Fatal("expected error removing nonexistent worktree")
	}
}

func TestAddDuplicate(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping git worktree test in short mode")
	}

	repo := setupTestRepo(t)
	createBranch(t, repo, "dup-branch")

	_ = Add(repo, "dup-branch")

	err := Add(repo, "dup-branch")
	if err == nil {
		t.Fatal("expected error adding duplicate worktree")
	}

	_ = Remove(repo, "dup-branch")
}
