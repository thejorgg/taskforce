package runner

import (
	"context"
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
