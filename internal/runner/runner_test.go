package runner

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/thejorgg/taskforce/internal/domain"
)

func TestShellStringRuns(t *testing.T) {
	r := Runner{Options: Options{Timeout: time.Minute}}
	res := r.Run(context.Background(), domain.CommandSpec{Name: "test", Run: "echo hello"})
	if res.ExitCode != 0 {
		t.Fatalf("exit=%d err=%s", res.ExitCode, res.Error)
	}
	if res.Stdout == "" {
		t.Fatal("expected stdout")
	}
}

func TestMutatingCommandRequiresApproval(t *testing.T) {
	r := Runner{Options: Options{Timeout: time.Minute}}
	res := r.Run(context.Background(), domain.CommandSpec{Name: "mutate", Run: "echo no", Mutates: true})
	if !res.Skipped {
		t.Fatal("expected skipped mutating command")
	}
}

func TestArgvRuns(t *testing.T) {
	r := Runner{Options: Options{Timeout: time.Minute}}
	res := r.Run(context.Background(), domain.CommandSpec{Name: "argv", Argv: []string{"go", "version"}})
	if res.ExitCode != 0 {
		t.Fatalf("exit=%d err=%s", res.ExitCode, res.Error)
	}
}

func TestRunnerStreamsOutputAndKeepsFinalBuffers(t *testing.T) {
	var chunks []string
	r := Runner{Options: Options{
		Timeout: time.Minute,
		OnOutput: func(name, stream, text string) {
			if name != "stream" || stream != "stdout" {
				t.Fatalf("unexpected stream event %s %s", name, stream)
			}
			chunks = append(chunks, text)
		},
	}}
	spec := domain.CommandSpec{Name: "stream", Run: "printf first; printf second"}
	if runtime.GOOS == "windows" {
		spec.Run = "echo first&&echo second"
	}
	res := r.Run(context.Background(), spec)
	if res.ExitCode != 0 {
		t.Fatalf("exit=%d err=%s", res.ExitCode, res.Error)
	}
	if !strings.Contains(res.Stdout, "first") || !strings.Contains(res.Stdout, "second") {
		t.Fatalf("stdout = %q", res.Stdout)
	}
	if !strings.Contains(strings.Join(chunks, ""), "first") {
		t.Fatalf("chunks = %#v", chunks)
	}
}

func TestWorkDirResolvesRelativeToRepo(t *testing.T) {
	repo := t.TempDir()
	r := Runner{Options: Options{Repo: repo}}
	if got := r.workDir("api"); got != filepath.Join(repo, "api") {
		t.Fatalf("relative work dir = %q", got)
	}
	abs := filepath.Join(repo, "cmd")
	if got := r.workDir(abs); got != abs {
		t.Fatalf("absolute work dir = %q", got)
	}
	if got := r.workDir("{{repo}}/packages/web"); got != filepath.Join(repo, "packages", "web") {
		t.Fatalf("repo placeholder work dir = %q", got)
	}
}
