package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/daemon"
	"github.com/thejorgg/taskforce/internal/domain"
	"github.com/thejorgg/taskforce/internal/orchestration"
	"github.com/thejorgg/taskforce/internal/runner"
	"github.com/thejorgg/taskforce/internal/tui"
)

func runCmd(args []string, smoke bool) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	cfgPath := fs.String("config", "", "explicit config override path")
	repo := fs.String("repo", ".", "repository path")
	signal := fs.String("signal", "", "task signal")
	signalFile := fs.String("signal-file", "", "file containing task signal")
	noTUI := fs.Bool("no-tui", false, "print progress lines and JSON instead of launching the TUI")
	yes := fs.Bool("yes", false, "confirm mutating commands, including the release gate")
	yolo := fs.Bool("yolo", false, "run configured mutating commands without confirmation")
	detach := fs.Bool("detach", false, "submit the run to the daemon and return immediately")
	local := fs.Bool("local", false, "run the pipeline in-process without the daemon")
	if err := fs.Parse(args); err != nil {
		return err
	}
	absRepo, err := filepath.Abs(*repo)
	if err != nil {
		return err
	}
	text := *signal
	if *signalFile != "" {
		data, err := os.ReadFile(*signalFile)
		if err != nil {
			return err
		}
		text = string(data)
	}
	if smoke {
		if text == "" {
			text = "Smoke test TaskForce pipeline wiring."
		}
		cfg, _, err := config.LoadEffective(absRepo, *cfgPath)
		if err != nil {
			return err
		}
		return runLocal(absRepo, smokeConfig(cfg), text, true, *yolo, *noTUI)
	}
	if text == "" {
		return errors.New("missing --signal or --signal-file")
	}
	cfg, _, err := config.LoadEffective(absRepo, *cfgPath)
	if err != nil {
		return err
	}
	if err := config.Validate(cfg); err != nil {
		return err
	}
	queueConfig, err := daemonConfigPath(*cfgPath)
	if err != nil {
		return err
	}
	if *local {
		return runLocal(absRepo, cfg, text, *yes, *yolo, *noTUI)
	}
	if _, err := daemon.Start(absRepo); err != nil {
		return err
	}
	record, err := daemon.SubmitRun(absRepo, daemon.JobOptions{Config: queueConfig, Yes: *yes, Yolo: *yolo}, "cli", text)
	if err != nil {
		return err
	}
	if *detach {
		fmt.Printf("run %s submitted\nfollow with: taskforce logs --follow %s\n", record.ID, record.ID)
		return nil
	}
	if *noTUI {
		final, err := watchRun(absRepo, record.ID, os.Stdout)
		if err != nil {
			return err
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(final)
	}
	return tui.ShowRun(absRepo, record.ID)
}

func daemonConfigPath(path string) (string, error) {
	if path == "" {
		return "", nil
	}
	return filepath.Abs(path)
}

// runLocal executes the pipeline in this process without the daemon.
func runLocal(repo string, cfg config.Config, text string, yes, yolo, noTUI bool) error {
	timeout, _ := time.ParseDuration(cfg.Runtime.Timeout)
	opts := runner.Options{
		Repo:    repo,
		Shell:   cfg.Runtime.Shell,
		Timeout: timeout,
		Env:     cfg.Runtime.Env,
		Yes:     yes,
		Yolo:    yolo,
		NoTUI:   noTUI,
	}
	p := orchestration.New(orchestration.Options{Config: cfg, Repo: repo, Runner: runner.Runner{Options: opts}})
	result := p.RunText(context.Background(), "cli", text)
	if noTUI {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	return tui.Show(result)
}

// watchRun polls a daemon-owned run, printing stage transitions until it
// finishes. When the run pauses at the release gate it prompts the operator
// on a TTY, or prints the approve/deny commands otherwise.
func watchRun(repo, id string, out io.Writer) (daemon.RunRecord, error) {
	last := map[domain.StageName]domain.StageStatus{}
	prompted := false
	for {
		record, ok, err := daemon.ReadRun(repo, id)
		if err != nil {
			return record, err
		}
		if ok {
			for _, stage := range record.Run.Stages {
				if stage.Status != domain.StatusIdle && last[stage.Name] != stage.Status {
					last[stage.Name] = stage.Status
					detail := ""
					if n := len(stage.Logs); n > 0 {
						detail = " · " + firstLine(stage.Logs[n-1])
					}
					fmt.Fprintf(out, "%-9s %s%s\n", strings.ToLower(string(stage.Name)), stage.Status, detail)
				}
			}
			if record.Status == daemon.RunAwaitingApproval && record.Pending != nil && !prompted {
				prompted = true
				if err := promptApproval(repo, record, out); err != nil {
					return record, err
				}
			}
			if record.Status != daemon.RunAwaitingApproval {
				prompted = false
			}
			if !record.Status.Active() {
				fmt.Fprintf(out, "run %s finished: %s\n", record.ID, record.Status)
				return record, nil
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
}

func promptApproval(repo string, record daemon.RunRecord, out io.Writer) error {
	fmt.Fprintf(out, "release gate: %s wants to run: %s\n", record.Pending.Stage, record.Pending.Command)
	if !stdinIsTerminal() {
		fmt.Fprintf(out, "operator decision required: taskforce approve %s | taskforce deny %s\n", record.ID, record.ID)
		return nil
	}
	fmt.Fprint(out, "approve? [y/N] ")
	reader := bufio.NewReader(os.Stdin)
	answer, _ := reader.ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "y" || answer == "yes" {
		return daemon.Approve(repo, record.ID, "approved from terminal")
	}
	return daemon.Deny(repo, record.ID, "denied from terminal")
}

func stdinIsTerminal() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			if len(line) > 100 {
				return line[:97] + "..."
			}
			return line
		}
	}
	return ""
}

func smokeConfig(cfg config.Config) config.Config {
	cfg.Pipeline.Scout.Enabled = false
	cfg.Relay.Control.Enabled = true
	cfg.Relay.Control.Agent = ""
	cfg.Relay.Control.Run = "echo TaskForce smoke: control planned"
	cfg.Relay.Control.Argv = nil
	cfg.Relay.Build.Enabled = true
	cfg.Relay.Build.Agent = ""
	cfg.Relay.Build.Run = "echo TaskForce smoke: build executed"
	cfg.Relay.Build.Argv = nil
	cfg.Scope.Hooks = []config.HookConfig{{Name: "smoke", Run: "echo TaskForce smoke: scope passed", Required: true}}
	cfg.Exfil.Commit = false
	cfg.Exfil.Push = false
	cfg.Exfil.PR = false
	cfg.Exfil.Branch = ""
	cfg.Exfil.Hooks = nil
	return cfg
}
