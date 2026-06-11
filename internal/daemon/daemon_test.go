package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/thejorgg/taskforce/internal/domain"
	"github.com/thejorgg/taskforce/internal/runner"
)

func TestSubmitProcessPendingWritesResultAndEvents(t *testing.T) {
	repo := t.TempDir()
	spec := domain.CommandSpec{Name: "scope.test", Run: "printf daemon-output"}
	job, err := Submit(repo, runner.Options{Timeout: time.Minute, Yes: true}, spec)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	if err := processPending(context.Background(), repo, &wg); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	done, ok, err := ReadJob(repo, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("job not found")
	}
	if done.Status != JobPassed {
		t.Fatalf("status = %s", done.Status)
	}
	if done.Result == nil || !strings.Contains(done.Result.Stdout, "daemon-output") {
		t.Fatalf("result = %#v", done.Result)
	}
	events, err := ReadEvents(repo, job.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || !strings.Contains(events[0].Text, "daemon-output") {
		t.Fatalf("events = %#v", events)
	}
}

func TestFailedResultCarriesCommandError(t *testing.T) {
	res := failedResult(domain.CommandSpec{Name: "x", Run: "echo x"}, context.Canceled)
	if res.ExitCode == 0 {
		t.Fatal("failed result exit code should be non-zero")
	}
	if !strings.Contains(res.Error, "context canceled") {
		t.Fatalf("error = %q", res.Error)
	}
}

func testRepoConfig(t *testing.T, body string) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "taskforce.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo
}

const echoPipelineConfig = `{
  "relay": {
    "control": {"enabled": true, "run": "echo planned"},
    "build": {"enabled": true, "run": "echo built"}
  },
  "scope": {"hooks": [{"name": "check", "run": "echo checked", "required": true}]},
  "exfil": {"branch": "", "commit": false, "push": false, "pr": false}
}`

func TestRunLifecyclePasses(t *testing.T) {
	repo := testRepoConfig(t, echoPipelineConfig)
	record, err := SubmitRun(repo, JobOptions{Timeout: "1m"}, "test", "Fix the login button")
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != RunPending {
		t.Fatalf("submitted status = %s", record.Status)
	}
	var wg sync.WaitGroup
	if err := processRunQueue(context.Background(), repo, &wg); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	final, ok, err := ReadRun(repo, record.ID)
	if err != nil || !ok {
		t.Fatalf("read run: ok=%v err=%v", ok, err)
	}
	if final.Status != RunPassed {
		t.Fatalf("status = %s, error = %s", final.Status, final.Error)
	}
	if final.Run.Relay.BuildResult == nil || !strings.Contains(final.Run.Relay.BuildResult.Stdout, "built") {
		t.Fatalf("relay result = %#v", final.Run.Relay.BuildResult)
	}
	if final.Run.Review.Status != domain.ReviewApproved {
		t.Fatalf("review = %#v", final.Run.Review)
	}
	if final.EndedAt.IsZero() {
		t.Fatal("ended_at not set")
	}
	runs, err := ListRuns(repo, 10)
	if err != nil || len(runs) != 1 {
		t.Fatalf("list runs = %#v err=%v", runs, err)
	}
}

func TestRunFailureFromFailingBuild(t *testing.T) {
	repo := testRepoConfig(t, `{
  "relay": {
    "control": {"enabled": false},
    "build": {"enabled": true, "run": "sh -c 'echo broken >&2; exit 3'"}
  },
  "scope": {"hooks": []},
  "exfil": {"branch": "", "commit": false}
}`)
	record, err := SubmitRun(repo, JobOptions{Timeout: "1m"}, "test", "Do a thing")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	if err := processRunQueue(context.Background(), repo, &wg); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	final, _, err := ReadRun(repo, record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != RunFailed {
		t.Fatalf("status = %s", final.Status)
	}
}

const approvalPipelineConfig = `{
  "relay": {
    "control": {"enabled": false},
    "build": {"enabled": true, "run": "echo built"}
  },
  "scope": {"hooks": []},
  "exfil": {"branch": "", "commit": false, "push": false, "pr": false,
    "hooks": [{"name": "release", "run": "echo released", "required": true}]}
}`

func TestRunPausesForApprovalAndApproves(t *testing.T) {
	repo := testRepoConfig(t, approvalPipelineConfig)
	record, err := SubmitRun(repo, JobOptions{Timeout: "1m"}, "test", "Ship it")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	if err := processRunQueue(context.Background(), repo, &wg); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, repo, record.ID, RunAwaitingApproval)
	pending, _, _ := ReadRun(repo, record.ID)
	if pending.Pending == nil || pending.Pending.Stage != "exfil.release" {
		t.Fatalf("pending = %#v", pending.Pending)
	}
	if err := Approve(repo, record.ID, "lgtm"); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	final, _, _ := ReadRun(repo, record.ID)
	if final.Status != RunPassed {
		t.Fatalf("status = %s, error = %s", final.Status, final.Error)
	}
	found := false
	for _, res := range final.Run.Release.Results {
		if strings.Contains(res.Stdout, "released") {
			found = true
		}
	}
	if !found {
		t.Fatalf("release hook did not run: %#v", final.Run.Release.Results)
	}
}

func TestRunPausesForApprovalAndDenies(t *testing.T) {
	repo := testRepoConfig(t, approvalPipelineConfig)
	record, err := SubmitRun(repo, JobOptions{Timeout: "1m"}, "test", "Ship it")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	if err := processRunQueue(context.Background(), repo, &wg); err != nil {
		t.Fatal(err)
	}
	waitForStatus(t, repo, record.ID, RunAwaitingApproval)
	if err := Deny(repo, record.ID, "not yet"); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	final, _, _ := ReadRun(repo, record.ID)
	if final.Status != RunDenied {
		t.Fatalf("status = %s", final.Status)
	}
}

func TestYesSkipsApprovalGate(t *testing.T) {
	repo := testRepoConfig(t, approvalPipelineConfig)
	record, err := SubmitRun(repo, JobOptions{Timeout: "1m", Yes: true}, "test", "Ship it")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	if err := processRunQueue(context.Background(), repo, &wg); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	final, _, _ := ReadRun(repo, record.ID)
	if final.Status != RunPassed {
		t.Fatalf("status = %s, error = %s", final.Status, final.Error)
	}
}

func TestApproveUnknownRunFails(t *testing.T) {
	repo := t.TempDir()
	if err := Approve(repo, "tf-missing", ""); err == nil {
		t.Fatal("approving a missing run must fail")
	}
}

func TestApproveBeforeApprovalGateFails(t *testing.T) {
	repo := testRepoConfig(t, approvalPipelineConfig)
	record, err := SubmitRun(repo, JobOptions{Timeout: "1m"}, "test", "Ship it")
	if err != nil {
		t.Fatal(err)
	}
	if err := Approve(repo, record.ID, "too early"); err == nil {
		t.Fatal("approving before the release gate must fail")
	}
	if _, ok, err := readDecision(repo, record.ID); err != nil || ok {
		t.Fatalf("early approval decision persisted: ok=%v err=%v", ok, err)
	}
}

func waitForStatus(t *testing.T, repo, id string, want RunStatus) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		record, ok, err := ReadRun(repo, id)
		if err != nil {
			t.Fatal(err)
		}
		if ok && record.Status == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	record, _, _ := ReadRun(repo, id)
	t.Fatalf("run never reached %s; last = %s (%s)", want, record.Status, record.Error)
}
