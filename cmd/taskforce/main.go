package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/orchestration"
	"github.com/thejorgg/taskforce/internal/runner"
	"github.com/thejorgg/taskforce/internal/tui"
)

const version = "v0.1"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "taskforce:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		repo, err := filepath.Abs(".")
		if err != nil {
			return err
		}
		return tui.ShowIdle(repo)
	}
	switch args[0] {
	case "init":
		return initCmd(args[1:])
	case "config":
		return configCmd(args[1:])
	case "run":
		return runCmd(args[1:], false)
	case "smoke":
		return runCmd(args[1:], true)
	case "version", "--version", "-v":
		fmt.Println(version)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func initCmd(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	path := fs.String("config", "taskforce.json", "config path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if _, err := os.Stat(*path); err == nil {
		return fmt.Errorf("%s already exists", *path)
	}
	return config.WriteDefault(*path)
}

func configCmd(args []string) error {
	if len(args) == 0 || args[0] != "check" {
		return errors.New("usage: taskforce config check [--config taskforce.json]")
	}
	fs := flag.NewFlagSet("config check", flag.ContinueOnError)
	path := fs.String("config", "taskforce.json", "config path")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	cfg, err := config.Load(*path)
	if err != nil {
		return err
	}
	if err := config.Validate(cfg); err != nil {
		return err
	}
	fmt.Println("config ok")
	return nil
}

func runCmd(args []string, smoke bool) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	cfgPath := fs.String("config", "taskforce.json", "config path")
	repo := fs.String("repo", ".", "repository path")
	signal := fs.String("signal", "", "task signal")
	signalFile := fs.String("signal-file", "", "file containing task signal")
	noTUI := fs.Bool("no-tui", false, "print JSON instead of launching the TUI")
	yes := fs.Bool("yes", false, "confirm mutating commands")
	yolo := fs.Bool("yolo", false, "run configured mutating commands without confirmation")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return err
	}
	if smoke {
		cfg = smokeConfig(cfg)
		if *signal == "" {
			*signal = "Smoke test TaskForce pipeline wiring."
		}
		*yes = true
	} else if err := config.Validate(cfg); err != nil {
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
	if text == "" {
		return errors.New("missing --signal or --signal-file")
	}
	absRepo, err := filepath.Abs(*repo)
	if err != nil {
		return err
	}
	timeout, _ := time.ParseDuration(cfg.Runtime.Timeout)
	r := runner.Runner{Options: runner.Options{
		Repo:    absRepo,
		Shell:   cfg.Runtime.Shell,
		Timeout: timeout,
		Env:     cfg.Runtime.Env,
		Yes:     *yes,
		Yolo:    *yolo,
		NoTUI:   *noTUI,
		DryRun:  false,
	}}
	p := orchestration.New(orchestration.Options{Config: cfg, Repo: absRepo, Runner: r})
	result := p.RunText(context.Background(), "cli", text)
	if *noTUI {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	return tui.Show(result)
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

func usage() {
	fmt.Println(`TaskForce v0.1

Usage:
  taskforce init [--config taskforce.json]
  taskforce config check [--config taskforce.json]
  taskforce run --signal "..." --repo PATH [--no-tui] [--yes|--yolo]
  taskforce run --signal-file PATH --repo PATH [--no-tui] [--yes|--yolo]
  taskforce smoke [--no-tui]
  taskforce
  taskforce version`)
}
