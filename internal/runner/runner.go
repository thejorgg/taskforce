package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/thejorgg/taskforce/internal/domain"
)

type Options struct {
	Repo      string
	Shell     string
	Timeout   time.Duration
	Env       map[string]string
	Yes       bool
	Yolo      bool
	NoTUI     bool
	DryRun    bool
	Confirm   func(domain.CommandSpec) bool
	OnCommand func(domain.CommandSpec)
	OnOutput  func(name, stream, text string)
	Executor  func(context.Context, domain.CommandSpec) domain.CommandResult
}

type Runner struct {
	Options Options
}

func (r Runner) Run(ctx context.Context, spec domain.CommandSpec) domain.CommandResult {
	start := time.Now()
	result := domain.CommandResult{
		Name:      spec.Name,
		StartedAt: start,
	}
	if spec.Run == "" && len(spec.Argv) == 0 {
		spec.Run = fmt.Sprintf("echo TaskForce: no command configured for %s; refusing to guess.", spec.Name)
	}
	result.Command = describe(spec)
	if spec.Mutates && !r.Options.Yolo && !r.Options.Yes {
		if r.Options.Confirm == nil || !r.Options.Confirm(spec) {
			result.Skipped = true
			result.ExitCode = 0
			result.EndedAt = time.Now()
			result.Duration = result.EndedAt.Sub(start)
			result.Stdout = "skipped: mutating command requires --yes, --yolo, or interactive confirmation"
			return result
		}
	}
	if r.Options.DryRun {
		result.Skipped = true
		result.ExitCode = 0
		result.EndedAt = time.Now()
		result.Duration = result.EndedAt.Sub(start)
		result.Stdout = "dry-run: " + describe(spec)
		return result
	}
	if r.Options.OnCommand != nil {
		r.Options.OnCommand(spec)
	}
	if r.Options.Executor != nil {
		return r.Options.Executor(ctx, spec)
	}
	timeout := r.Options.Timeout
	if spec.Timeout != "" {
		if parsed, err := time.ParseDuration(spec.Timeout); err == nil {
			timeout = parsed
		}
	}
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := r.command(runCtx, spec)
	cmd.Dir = r.workDir(spec.WorkDir)
	cmd.Env = mergedEnv(r.Options.Env, spec.Env)
	var stdout, stderr bytes.Buffer
	err := runCommand(cmd, spec.Name, &stdout, &stderr, r.Options.OnOutput)
	result.EndedAt = time.Now()
	result.Duration = result.EndedAt.Sub(start)
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	result.ExitCode = exitCode(err)
	if err != nil {
		result.Error = err.Error()
		if errors.Is(runCtx.Err(), context.DeadlineExceeded) {
			result.Error = "command timed out: " + err.Error()
		}
	}
	return result
}

func runCommand(cmd *exec.Cmd, name string, stdout, stderr *bytes.Buffer, onOutput func(name, stream, text string)) error {
	if onOutput == nil {
		cmd.Stdout = stdout
		cmd.Stderr = stderr
		return cmd.Run()
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	var wg sync.WaitGroup
	wg.Add(2)
	go copyStream(&wg, stdout, stdoutPipe, func(text string) { onOutput(name, "stdout", text) })
	go copyStream(&wg, stderr, stderrPipe, func(text string) { onOutput(name, "stderr", text) })
	wg.Wait()
	return cmd.Wait()
}

func copyStream(wg *sync.WaitGroup, buf *bytes.Buffer, src io.Reader, emit func(string)) {
	defer wg.Done()
	var chunk [4096]byte
	for {
		n, err := src.Read(chunk[:])
		if n > 0 {
			text := string(chunk[:n])
			buf.WriteString(text)
			emit(text)
		}
		if err != nil {
			return
		}
	}
}

func (r Runner) command(ctx context.Context, spec domain.CommandSpec) *exec.Cmd {
	if len(spec.Argv) > 0 {
		return exec.CommandContext(ctx, spec.Argv[0], spec.Argv[1:]...)
	}
	shell, args := shellCommand(r.Options.Shell, spec.Run)
	return exec.CommandContext(ctx, shell, args...)
}

func (r Runner) workDir(configured string) string {
	dir := strings.ReplaceAll(configured, "{{repo}}", r.Options.Repo)
	if dir == "" {
		return r.Options.Repo
	}
	if filepath.IsAbs(dir) || r.Options.Repo == "" {
		return dir
	}
	return filepath.Join(r.Options.Repo, dir)
}

func shellCommand(configured, script string) (string, []string) {
	if configured != "" {
		if runtime.GOOS == "windows" {
			return configured, []string{"/C", script}
		}
		return configured, []string{"-c", script}
	}
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", script}
	}
	return "/bin/sh", []string{"-c", script}
}

func mergedEnv(global, local map[string]string) []string {
	env := os.Environ()
	for k, v := range global {
		env = append(env, k+"="+v)
	}
	for k, v := range local {
		env = append(env, k+"="+v)
	}
	return env
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	return 127
}

func describe(spec domain.CommandSpec) string {
	if len(spec.Argv) > 0 {
		return strings.Join(spec.Argv, " ")
	}
	return spec.Run
}
