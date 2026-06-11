package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/domain"
	"github.com/thejorgg/taskforce/internal/orchestration"
	"github.com/thejorgg/taskforce/internal/runner"
)

// RunStatus tracks a pipeline run owned by the daemon.
type RunStatus string

const (
	RunPending          RunStatus = "pending"
	RunRunning          RunStatus = "running"
	RunAwaitingApproval RunStatus = "awaiting_approval"
	RunPassed           RunStatus = "passed"
	RunNeedsRevision    RunStatus = "needs_revision"
	RunFailed           RunStatus = "failed"
	RunDenied           RunStatus = "denied"
)

// Active reports whether the run is still in flight.
func (s RunStatus) Active() bool {
	return s == RunPending || s == RunRunning || s == RunAwaitingApproval
}

// RunRecord is the persisted state of one pipeline run under .taskforce/runs.
type RunRecord struct {
	ID        string             `json:"id"`
	Repo      string             `json:"repo"`
	Source    string             `json:"source"`
	Signal    string             `json:"signal"`
	Status    RunStatus          `json:"status"`
	Options   JobOptions         `json:"options"`
	Pending   *ApprovalRequest   `json:"pending,omitempty"`
	Run       domain.PipelineRun `json:"run"`
	Error     string             `json:"error,omitempty"`
	DaemonPID int                `json:"daemon_pid,omitempty"`
	CreatedAt time.Time          `json:"created_at"`
	StartedAt time.Time          `json:"started_at,omitempty"`
	EndedAt   time.Time          `json:"ended_at,omitempty"`
}

// ApprovalRequest describes the mutating command a run is paused on.
type ApprovalRequest struct {
	RunID       string    `json:"run_id"`
	Stage       string    `json:"stage"`
	Command     string    `json:"command"`
	RequestedAt time.Time `json:"requested_at"`
}

// ApprovalDecision is the operator answer for a paused run.
type ApprovalDecision struct {
	RunID     string    `json:"run_id"`
	Decision  string    `json:"decision"`
	Reason    string    `json:"reason,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

const (
	DecisionApprove = "approve"
	DecisionDeny    = "deny"
)

// SubmitRun queues a pipeline run for the daemon to execute.
func SubmitRun(repo string, opts JobOptions, source, text string) (RunRecord, error) {
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return RunRecord{}, err
	}
	if err := os.MkdirAll(runQueueDir(absRepo), 0o755); err != nil {
		return RunRecord{}, err
	}
	now := time.Now()
	record := RunRecord{
		ID:        "tf-" + strconv.FormatInt(now.UnixNano(), 36),
		Repo:      absRepo,
		Source:    source,
		Signal:    text,
		Status:    RunPending,
		Options:   opts,
		CreatedAt: now,
	}
	return record, writeJSONAtomic(runQueuePath(absRepo, record.ID), record)
}

// ReadRun loads one run record by ID, checking active and queued runs.
func ReadRun(repo, id string) (RunRecord, bool, error) {
	data, err := os.ReadFile(runPath(repo, id))
	if errors.Is(err, os.ErrNotExist) {
		data, err = os.ReadFile(runQueuePath(repo, id))
	}
	if errors.Is(err, os.ErrNotExist) {
		return RunRecord{}, false, nil
	}
	if err != nil {
		return RunRecord{}, false, err
	}
	var record RunRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return RunRecord{}, false, err
	}
	return record, true, nil
}

// ListRuns returns recent runs sorted oldest first. limit <= 0 means all.
func ListRuns(repo string, limit int) ([]RunRecord, error) {
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	records := []RunRecord{}
	for _, dir := range []string{runsDir(absRepo), runQueueDir(absRepo)} {
		paths, err := filepath.Glob(filepath.Join(dir, "*.json"))
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			var record RunRecord
			if err := json.Unmarshal(data, &record); err != nil {
				continue
			}
			records = append(records, record)
		}
	}
	sort.Slice(records, func(i, j int) bool { return records[i].CreatedAt.Before(records[j].CreatedAt) })
	if limit > 0 && len(records) > limit {
		records = records[len(records)-limit:]
	}
	return records, nil
}

// ReadRunEvents returns the streamed command output for a run.
func ReadRunEvents(repo, id string) ([]JobEvent, error) {
	return readEventsFile(filepath.Join(runsDir(repo), id+".jsonl"))
}

// Approve records an operator approval for a run's pending mutating commands.
func Approve(repo, id, reason string) error {
	return decide(repo, id, DecisionApprove, reason)
}

// Deny records an operator denial; the run's mutating commands are skipped.
func Deny(repo, id, reason string) error {
	return decide(repo, id, DecisionDeny, reason)
}

func decide(repo, id, decision, reason string) error {
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return err
	}
	record, ok, err := ReadRun(absRepo, id)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("run %s not found", id)
	}
	if !record.Status.Active() {
		return fmt.Errorf("run %s already finished with status %s", id, record.Status)
	}
	if record.Status != RunAwaitingApproval || record.Pending == nil {
		return fmt.Errorf("run %s is not awaiting approval", id)
	}
	if err := os.MkdirAll(approvalsDir(absRepo), 0o755); err != nil {
		return err
	}
	return writeJSONAtomic(approvalPath(absRepo, id), ApprovalDecision{
		RunID:     id,
		Decision:  decision,
		Reason:    reason,
		CreatedAt: time.Now(),
	})
}

func readDecision(repo, id string) (ApprovalDecision, bool, error) {
	data, err := os.ReadFile(approvalPath(repo, id))
	if errors.Is(err, os.ErrNotExist) {
		return ApprovalDecision{}, false, nil
	}
	if err != nil {
		return ApprovalDecision{}, false, err
	}
	var decision ApprovalDecision
	if err := json.Unmarshal(data, &decision); err != nil {
		return ApprovalDecision{}, false, err
	}
	return decision, true, nil
}

func processRunQueue(ctx context.Context, wg *sync.WaitGroup) error {
	base := reposBase()
	if base == "" {
		return nil
	}
	repoDirs, err := filepath.Glob(filepath.Join(base, "*"))
	if err != nil {
		return err
	}
	for _, repoDir := range repoDirs {
		if err := processRepoRunQueue(ctx, repoDir, wg); err != nil {
			appendDaemonLog(repoDir, "run queue error: "+err.Error())
		}
	}
	return nil
}

func processRepoRunQueue(ctx context.Context, repoDir string, wg *sync.WaitGroup) error {
	paths, err := filepath.Glob(filepath.Join(repoDir, "runqueue", "*.json"))
	if err != nil {
		return err
	}
	sort.Strings(paths)
	for _, path := range paths {
		// Claim the queue file by renaming it: exactly one process wins the
		// rename, so concurrent daemons cannot execute the same run twice.
		claim := path + ".claim"
		if err := os.Rename(path, claim); err != nil {
			continue
		}
		data, err := os.ReadFile(claim)
		if err != nil {
			return err
		}
		var record RunRecord
		if err := json.Unmarshal(data, &record); err != nil {
			_ = os.Remove(claim)
			return err
		}
		if err := os.MkdirAll(filepath.Join(repoDir, "runs"), 0o755); err != nil {
			return err
		}
		record.Status = RunRunning
		record.StartedAt = time.Now()
		record.DaemonPID = os.Getpid()
		if err := writeJSONAtomic(filepath.Join(repoDir, "runs", record.ID+".json"), record); err != nil {
			return err
		}
		_ = os.Remove(claim)
		wg.Add(1)
		go func(rec RunRecord) {
			defer wg.Done()
			executeRun(ctx, record.Repo, rec)
		}(record)
	}
	return nil
}

func executeRun(ctx context.Context, repo string, record RunRecord) {
	finish := func(status RunStatus, errText string) {
		record.Status = status
		record.Error = errText
		record.EndedAt = time.Now()
		_ = writeJSONAtomic(runPath(repo, record.ID), record)
	}
	cfg, _, err := config.LoadEffective(repo, record.Options.Config)
	if err != nil {
		finish(RunFailed, "config: "+err.Error())
		return
	}
	if err := config.Validate(cfg); err != nil {
		finish(RunFailed, "config: "+err.Error())
		return
	}
	timeout, _ := time.ParseDuration(record.Options.Timeout)
	if timeout <= 0 {
		timeout, _ = time.ParseDuration(cfg.Runtime.Timeout)
	}
	shell := record.Options.Shell
	if shell == "" {
		shell = cfg.Runtime.Shell
	}
	approved, denied := false, false
	opts := runner.Options{
		Repo:    repo,
		Shell:   shell,
		Timeout: timeout,
		Env:     mergeStringMaps(cfg.Runtime.Env, record.Options.Env),
		Yes:     record.Options.Yes,
		Yolo:    record.Options.Yolo,
		OnOutput: func(name, stream, text string) {
			_ = appendEventFile(filepath.Join(runsDir(repo), record.ID+".jsonl"), JobEvent{
				JobID: record.ID, Command: name, Stream: stream, Text: text, CreatedAt: time.Now(),
			})
		},
		Confirm: func(spec domain.CommandSpec) bool {
			// Submitting a run is implicit approval of the Relay
			// implementation loop; release-side commands (exfil.*)
			// wait for an explicit operator decision.
			if strings.HasPrefix(spec.Name, "relay.") {
				return true
			}
			if approved {
				return true
			}
			if denied {
				return false
			}
			if awaitApproval(ctx, repo, &record, spec) {
				approved = true
				return true
			}
			denied = true
			return false
		},
	}
	pipeline := orchestration.New(orchestration.Options{
		Config: cfg,
		Repo:   repo,
		Runner: runner.Runner{Options: opts},
		RunID:  record.ID,
		Observe: func(run domain.PipelineRun) {
			record.Run = run
			_ = writeJSONAtomic(runPath(repo, record.ID), record)
		},
	})
	run := pipeline.RunText(ctx, record.Source, record.Signal)
	record.Run = run
	if ctx.Err() != nil {
		finish(RunFailed, "interrupted: daemon stopped")
		return
	}
	finish(finalStatus(run, denied), "")
}

func awaitApproval(ctx context.Context, repo string, record *RunRecord, spec domain.CommandSpec) bool {
	record.Status = RunAwaitingApproval
	record.Pending = &ApprovalRequest{
		RunID:       record.ID,
		Stage:       spec.Name,
		Command:     commandText(spec),
		RequestedAt: time.Now(),
	}
	_ = writeJSONAtomic(runPath(repo, record.ID), *record)
	defer func() {
		record.Pending = nil
		record.Status = RunRunning
		_ = writeJSONAtomic(runPath(repo, record.ID), *record)
	}()
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	for {
		decision, ok, err := readDecision(repo, record.ID)
		if err == nil && ok {
			// Consume the decision so a stale file can never answer a
			// future gate.
			_ = os.Remove(approvalPath(repo, record.ID))
			return decision.Decision == DecisionApprove
		}
		select {
		case <-ctx.Done():
			return false
		case <-ticker.C:
		}
	}
}

func finalStatus(run domain.PipelineRun, denied bool) RunStatus {
	if denied {
		return RunDenied
	}
	for _, stage := range run.Stages {
		if stage.Status == domain.StatusFailed {
			return RunFailed
		}
		if stage.Status == domain.StatusNeedsRevision {
			return RunNeedsRevision
		}
	}
	return RunPassed
}

// recoverStaleRuns marks runs from a dead daemon as failed so they do not
// show as running forever after a crash or restart.
func recoverStaleRuns(repo string) {
	records, err := ListRuns(repo, 0)
	if err != nil {
		return
	}
	state, stateOK, _ := Status()
	for _, record := range records {
		if !record.Status.Active() || record.Status == RunPending {
			continue
		}
		if record.DaemonPID == os.Getpid() {
			continue
		}
		// A recycled PID alone is not proof of life: the owning daemon must
		// also be the daemon recorded in the state file with a fresh
		// heartbeat.
		ownerAlive := stateOK && state.Status == "running" &&
			state.PID == record.DaemonPID &&
			time.Since(state.Heartbeat) < 10*time.Second &&
			processAlive(record.DaemonPID)
		if ownerAlive {
			continue
		}
		record.Status = RunFailed
		record.Error = "interrupted: daemon stopped"
		record.EndedAt = time.Now()
		_ = writeJSONAtomic(runPath(repo, record.ID), record)
	}
}

func mergeStringMaps(maps ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

func runQueueDir(repo string) string {
	return filepath.Join(dir(repo), "runqueue")
}

func runsDir(repo string) string {
	return filepath.Join(dir(repo), "runs")
}

func approvalsDir(repo string) string {
	return filepath.Join(dir(repo), "approvals")
}

func runQueuePath(repo, id string) string {
	return filepath.Join(runQueueDir(repo), id+".json")
}

func runPath(repo, id string) string {
	return filepath.Join(runsDir(repo), id+".json")
}

func approvalPath(repo, id string) string {
	return filepath.Join(approvalsDir(repo), id+".json")
}
