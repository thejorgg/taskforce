package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveFindsGitRoot(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(sub)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Fatalf("Resolve(%q) = %q, want %q", sub, got, root)
	}
}

func TestResolveFindsTaskforceJSON(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "taskforce.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(sub)
	if err != nil {
		t.Fatal(err)
	}
	if got != root {
		t.Fatalf("Resolve(%q) = %q, want %q", sub, got, root)
	}
}

func TestResolveAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	got, err := Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Fatalf("Resolve(%q) = %q, want %q", dir, got, dir)
	}
}

func TestResolveRejectsNonexistent(t *testing.T) {
	_, err := Resolve("/no/such/path/ever")
	if err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestResolveFileReturnsDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Resolve(file)
	if err != nil {
		t.Fatal(err)
	}
	if got != dir {
		t.Fatalf("Resolve(%q) = %q, want %q", file, got, dir)
	}
}

func TestSaveLoadState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	s := State{ActiveRepo: "/tmp/myrepo"}
	if err := SaveState(s); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ActiveRepo != s.ActiveRepo {
		t.Fatalf("ActiveRepo = %q, want %q", loaded.ActiveRepo, s.ActiveRepo)
	}
}

func TestLoadStateMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	s, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if s.ActiveRepo != "" {
		t.Fatalf("ActiveRepo = %q, want empty", s.ActiveRepo)
	}
}

func TestSaveStateOverwrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))

	if err := SaveState(State{ActiveRepo: "/old"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveState(State{ActiveRepo: "/new"}); err != nil {
		t.Fatal(err)
	}
	s, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if s.ActiveRepo != "/new" {
		t.Fatalf("ActiveRepo = %q, want %q", s.ActiveRepo, "/new")
	}
}
