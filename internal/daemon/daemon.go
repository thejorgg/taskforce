// Package daemon owns local TaskForce state under .taskforce/: the heartbeat,
// queued command jobs, pipeline runs, approvals, and streamed process output.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/thejorgg/taskforce/internal/domain"
	"github.com/thejorgg/taskforce/internal/runner"
)

type State struct {
	PID       int       `json:"pid"`
	Repo      string    `json:"repo"`
	Status    string    `json:"status"`
	StartedAt time.Time `json:"started_at"`
	Heartbeat time.Time `json:"heartbeat"`
}

type JobStatus string

const (
	JobPending JobStatus = "pending"
	JobRunning JobStatus = "running"
	JobPassed  JobStatus = "passed"
	JobFailed  JobStatus = "failed"
)

type Job struct {
	ID        string                `json:"id"`
	Repo      string                `json:"repo"`
	Spec      domain.CommandSpec    `json:"spec"`
	Options   JobOptions            `json:"options"`
	Status    JobStatus             `json:"status"`
	Result    *domain.CommandResult `json:"result,omitempty"`
	CreatedAt time.Time             `json:"created_at"`
	StartedAt time.Time             `json:"started_at,omitempty"`
	EndedAt   time.Time             `json:"ended_at,omitempty"`
}

type JobOptions struct {
	Shell   string            `json:"shell,omitempty"`
	Timeout string            `json:"timeout,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Config  string            `json:"config,omitempty"`
	Yes     bool              `json:"yes,omitempty"`
	Yolo    bool              `json:"yolo,omitempty"`
}

type JobEvent struct {
	JobID     string    `json:"job_id"`
	Command   string    `json:"command"`
	Stream    string    `json:"stream"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

func Start(repo string) (State, error) {
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return State{}, err
	}
	if state, ok, err := Status(absRepo); err != nil {
		return State{}, err
	} else if ok && state.Status == "running" {
		return state, nil
	}
	if err := os.MkdirAll(dir(absRepo), 0o755); err != nil {
		return State{}, err
	}
	exe, err := os.Executable()
	if err != nil {
		return State{}, err
	}
	log, err := os.OpenFile(filepath.Join(dir(absRepo), "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return State{}, err
	}
	defer log.Close()
	cmd := exec.Command(exe, "daemon", "run", "--repo", absRepo)
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		return State{}, err
	}
	state := State{PID: cmd.Process.Pid, Repo: absRepo, Status: "running", StartedAt: time.Now(), Heartbeat: time.Now()}
	if err := writeState(absRepo, state); err != nil {
		_ = cmd.Process.Kill()
		return State{}, err
	}
	if err := cmd.Process.Release(); err != nil {
		return State{}, err
	}
	return state, nil
}

func Run(repo string) error {
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir(absRepo), 0o755); err != nil {
		return err
	}
	recoverStaleRuns(absRepo)
	state := State{PID: os.Getpid(), Repo: absRepo, Status: "running", StartedAt: time.Now(), Heartbeat: time.Now()}
	if err := writeState(absRepo, state); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	stop := make(chan os.Signal, 1)
	signalNotify(stop)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			state.Heartbeat = time.Now()
			if err := writeState(absRepo, state); err != nil {
				return err
			}
			if err := processPending(ctx, &wg); err != nil {
				appendDaemonLog(absRepo, "queue error: "+err.Error())
			}
			if err := processRunQueue(ctx, absRepo, &wg); err != nil {
				appendDaemonLog(absRepo, "run queue error: "+err.Error())
			}
		case <-stop:
			cancel()
			waitWithTimeout(&wg, 3*time.Second)
			state.Status = "stopped"
			state.Heartbeat = time.Now()
			return writeState(absRepo, state)
		}
	}
}

func waitWithTimeout(wg *sync.WaitGroup, timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// RunCommand submits a single command to the daemon and blocks until it
// completes, returning the result.
func RunCommand(ctx context.Context, repo string, opts runner.Options, spec domain.CommandSpec) domain.CommandResult {
	if _, err := Start(repo); err != nil {
		return failedResult(spec, err)
	}
	job, err := Submit(repo, opts, spec)
	if err != nil {
		return failedResult(spec, err)
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		current, ok, err := ReadJob(job.Repo, job.ID)
		if err != nil {
			return failedResult(spec, err)
		}
		if ok && current.Result != nil {
			return *current.Result
		}
		select {
		case <-ctx.Done():
			return failedResult(spec, ctx.Err())
		case <-ticker.C:
		}
	}
}

func Submit(repo string, opts runner.Options, spec domain.CommandSpec) (Job, error) {
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return Job{}, err
	}
	if err := os.MkdirAll(queueDir(absRepo), 0o755); err != nil {
		return Job{}, err
	}
	timeout := ""
	if opts.Timeout > 0 {
		timeout = opts.Timeout.String()
	}
	now := time.Now()
	job := Job{
		ID:        fmt.Sprintf("job-%d", now.UnixNano()),
		Repo:      absRepo,
		Spec:      spec,
		Options:   JobOptions{Shell: opts.Shell, Timeout: timeout, Env: opts.Env, Yes: opts.Yes, Yolo: opts.Yolo},
		Status:    JobPending,
		CreatedAt: now,
	}
	return job, writeJSONAtomic(queuePath(absRepo, job.ID), job)
}

func ReadJob(repo, id string) (Job, bool, error) {
	path := filepath.Join(jobsDir(repo), id+".json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		data, err = os.ReadFile(queuePath(repo, id))
	}
	if errors.Is(err, os.ErrNotExist) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, err
	}
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return Job{}, false, err
	}
	return job, true, nil
}

func RecentJobs(repo string, limit int) ([]Job, error) {
	paths, err := filepath.Glob(filepath.Join(jobsDir(repo), "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	if limit > 0 && len(paths) > limit {
		paths = paths[len(paths)-limit:]
	}
	jobs := make([]Job, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var job Job
		if err := json.Unmarshal(data, &job); err != nil {
			return nil, err
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

func ReadEvents(repo, jobID string) ([]JobEvent, error) {
	return readEventsFile(eventsPath(repo, jobID))
}

func readEventsFile(path string) ([]JobEvent, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lines := bytesLines(data)
	events := make([]JobEvent, 0, len(lines))
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		var event JobEvent
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		events = append(events, event)
	}
	return events, nil
}

func Stop(repo string) error {
	state, ok, err := Status(repo)
	if err != nil || !ok {
		return err
	}
	if state.PID > 0 {
		process, err := os.FindProcess(state.PID)
		if err == nil && processAlive(state.PID) {
			_ = process.Signal(os.Interrupt)
			deadline := time.Now().Add(5 * time.Second)
			for time.Now().Before(deadline) {
				if !processAlive(state.PID) {
					return markStopped(state.Repo, state)
				}
				time.Sleep(50 * time.Millisecond)
			}
			_ = process.Kill()
		}
	}
	return markStopped(state.Repo, state)
}

func Status(repo string) (State, bool, error) {
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return State{}, false, err
	}
	data, err := os.ReadFile(statePath(absRepo))
	if errors.Is(err, os.ErrNotExist) {
		return State{}, false, nil
	}
	if err != nil {
		return State{}, false, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, false, err
	}
	if state.PID <= 0 || !processAlive(state.PID) {
		state.Status = "stopped"
		_ = writeState(absRepo, state)
	}
	return state, true, nil
}

func markStopped(repo string, state State) error {
	state.Status = "stopped"
	state.Heartbeat = time.Now()
	return writeState(repo, state)
}

func writeState(repo string, state State) error {
	return writeJSONAtomic(statePath(repo), state)
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

func dir(repo string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(repo, ".taskforce")
	}
	repoSlug := strings.ReplaceAll(strings.TrimPrefix(repo, "/"), "/", "_")
	return filepath.Join(home, ".local", "share", "taskforce", "repos", repoSlug)
}

func reposBase() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "taskforce", "repos")
}

func statePath(repo string) string {
	return filepath.Join(dir(repo), "daemon.json")
}

func queueDir(repo string) string {
	return filepath.Join(dir(repo), "queue")
}

func jobsDir(repo string) string {
	return filepath.Join(dir(repo), "jobs")
}

func queuePath(repo, id string) string {
	return filepath.Join(queueDir(repo), id+".json")
}

func jobPath(repo, id string) string {
	return filepath.Join(jobsDir(repo), id+".json")
}

func eventsPath(repo, id string) string {
	return filepath.Join(jobsDir(repo), id+".jsonl")
}

func signalNotify(stop chan<- os.Signal) {
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
}

func Format(state State, ok bool) string {
	if !ok {
		return "local daemon stopped"
	}
	return fmt.Sprintf("local daemon %s · pid %d · heartbeat %s", state.Status, state.PID, state.Heartbeat.Format(time.RFC3339))
}

// processPending claims queued command jobs and executes each in its own
// goroutine so long commands do not stall the daemon heartbeat. It scans all
// tracked repos under the global repos directory.
func processPending(ctx context.Context, wg *sync.WaitGroup) error {
	base := reposBase()
	if base == "" {
		return nil
	}
	repoDirs, err := filepath.Glob(filepath.Join(base, "*"))
	if err != nil {
		return err
	}
	for _, repoDir := range repoDirs {
		if err := processRepoPending(ctx, repoDir, wg); err != nil {
			appendDaemonLog(repoDir, "queue error: "+err.Error())
		}
	}
	return nil
}

func processRepoPending(ctx context.Context, repoDir string, wg *sync.WaitGroup) error {
	paths, err := filepath.Glob(filepath.Join(repoDir, "queue", "*.json"))
	if err != nil {
		return err
	}
	sort.Strings(paths)
	for _, path := range paths {
		// Claim the queue file by renaming it: exactly one process wins the
		// rename, so concurrent daemons cannot execute the same job twice.
		claim := path + ".claim"
		if err := os.Rename(path, claim); err != nil {
			continue
		}
		data, err := os.ReadFile(claim)
		if err != nil {
			return err
		}
		var job Job
		if err := json.Unmarshal(data, &job); err != nil {
			_ = os.Remove(claim)
			return err
		}
		job.Status = JobRunning
		job.StartedAt = time.Now()
		if err := writeJSONAtomic(filepath.Join(repoDir, "jobs", job.ID+".json"), job); err != nil {
			return err
		}
		_ = os.Remove(claim)
		wg.Add(1)
		go func(j Job) {
			defer wg.Done()
			executeJobInDir(ctx, repoDir, j)
		}(job)
	}
	return nil
}

func executeJob(ctx context.Context, repo string, job Job) {
	timeout, _ := time.ParseDuration(job.Options.Timeout)
	r := runner.Runner{Options: runner.Options{
		Repo:    job.Repo,
		Shell:   job.Options.Shell,
		Timeout: timeout,
		Env:     job.Options.Env,
		Yes:     job.Options.Yes,
		Yolo:    job.Options.Yolo,
		OnOutput: func(name, stream, text string) {
			_ = appendEventFile(eventsPath(repo, job.ID), JobEvent{JobID: job.ID, Command: name, Stream: stream, Text: text, CreatedAt: time.Now()})
		},
	}}
	result := r.Run(ctx, job.Spec)
	job.Result = &result
	job.EndedAt = time.Now()
	if result.ExitCode == 0 {
		job.Status = JobPassed
	} else {
		job.Status = JobFailed
	}
	if err := writeJSONAtomic(jobPath(repo, job.ID), job); err != nil {
		appendDaemonLog(repo, "job write error: "+err.Error())
	}
}

func executeJobInDir(ctx context.Context, repoDir string, job Job) {
	timeout, _ := time.ParseDuration(job.Options.Timeout)
	r := runner.Runner{Options: runner.Options{
		Repo:    job.Repo,
		Shell:   job.Options.Shell,
		Timeout: timeout,
		Env:     job.Options.Env,
		Yes:     job.Options.Yes,
		Yolo:    job.Options.Yolo,
		OnOutput: func(name, stream, text string) {
			_ = appendEventFile(filepath.Join(repoDir, "jobs", job.ID+".jsonl"), JobEvent{JobID: job.ID, Command: name, Stream: stream, Text: text, CreatedAt: time.Now()})
		},
	}}
	result := r.Run(ctx, job.Spec)
	job.Result = &result
	job.EndedAt = time.Now()
	if result.ExitCode == 0 {
		job.Status = JobPassed
	} else {
		job.Status = JobFailed
	}
	if err := writeJSONAtomic(filepath.Join(repoDir, "jobs", job.ID+".json"), job); err != nil {
		appendDaemonLog(repoDir, "job write error: "+err.Error())
	}
}

// writeJSONAtomic writes JSON via a temp file and rename so readers polling
// the file never observe a partial document.
func writeJSONAtomic(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Rename(name, path)
}

func appendEventFile(path string, event JobEvent) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

func appendDaemonLog(repo, text string) {
	f, err := os.OpenFile(filepath.Join(dir(repo), "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = fmt.Fprintf(f, "%s %s\n", time.Now().Format(time.RFC3339), text)
}

func failedResult(spec domain.CommandSpec, err error) domain.CommandResult {
	now := time.Now()
	return domain.CommandResult{Name: spec.Name, Command: commandText(spec), ExitCode: 127, StartedAt: now, EndedAt: now, Error: err.Error()}
}

func commandText(spec domain.CommandSpec) string {
	if len(spec.Argv) > 0 {
		return fmt.Sprint(spec.Argv)
	}
	return spec.Run
}

func bytesLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, data[start:i])
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}
