package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/workspace"
)

func TestInitProfileAndWorkspaceSet(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)

	if err := run([]string{"init", "--level", "profile"}); err != nil {
		t.Fatal(err)
	}
	profile, err := config.ProfilePath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(profile); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"config", "set", "--level", "workspace", "relay.build.agent", "codex"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repo, ".taskforce", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"agent": "codex"`) {
		t.Fatalf("workspace config = %s", data)
	}
}

func TestConfigShowEffectivePrintsJSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	repo := filepath.Join(t.TempDir(), "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(repo)

	output := captureStdout(t, func() {
		if err := run([]string{"config", "show"}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, `"relay"`) || !strings.Contains(output, `"runtime"`) {
		t.Fatalf("output = %s", output)
	}
}

func TestSwitchResolvesAndPersists(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	repo := filepath.Join(t.TempDir(), "myrepo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	output := captureStdout(t, func() {
		if err := run([]string{"switch", repo}); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(output, "switched to") {
		t.Fatalf("output = %s", output)
	}
	state, err := workspace.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if state.ActiveRepo != repo {
		t.Fatalf("ActiveRepo = %q, want %q", state.ActiveRepo, repo)
	}
}

func TestSwitchRequiresArg(t *testing.T) {
	if err := run([]string{"switch"}); err == nil {
		t.Fatal("expected error for switch with no args")
	}
}

func TestSwitchRejectsBadPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	if err := run([]string{"switch", "/no/such/path/ever"}); err == nil {
		t.Fatal("expected error for nonexistent path")
	}
}

func TestDaemonConfigPathIsAbsolute(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	got, err := daemonConfigPath("taskforce.alt.json")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(dir, "taskforce.alt.json") {
		t.Fatalf("config path = %q", got)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	fn()
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	os.Stdout = old
	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}
