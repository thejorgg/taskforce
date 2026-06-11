// Command taskforce is a terminal command center for AI-assisted development
// pipelines: Echo -> Dispatch -> Relay -> Scope -> Exfil.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/daemon"
	"github.com/thejorgg/taskforce/internal/tui"
	"github.com/thejorgg/taskforce/internal/workspace"
)

const version = "v0.3"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "taskforce:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		repo := "."
		if state, err := workspace.LoadState(); err == nil && state.ActiveRepo != "" {
			if info, statErr := os.Stat(state.ActiveRepo); statErr == nil && info.IsDir() {
				repo = state.ActiveRepo
			}
		}
		absRepo, err := filepath.Abs(repo)
		if err != nil {
			return err
		}
		if _, err := daemon.Start(absRepo); err != nil {
			return err
		}
		return tui.ShowIdle(absRepo)
	}
	switch args[0] {
	case "init":
		return initCmd(args[1:])
	case "config":
		return configCmd(args[1:])
	case "daemon":
		return daemonCmd(args[1:])
	case "run":
		return runCmd(args[1:], false)
	case "smoke":
		return runCmd(args[1:], true)
	case "runs":
		return runsCmd(args[1:])
	case "logs":
		return logsCmd(args[1:])
	case "approve":
		return decideCmd(args[1:], true)
	case "deny":
		return decideCmd(args[1:], false)
	case "switch":
		return switchCmd(args[1:])
	case "agents":
		return agentsCmd(args[1:])
	case "worktree":
		return worktreeCmd(args[1:])
	case "doctor":
		return doctorCmd(args[1:])
	case "version", "--version", "-v":
		fmt.Println(version)
		return nil
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		return fmt.Errorf("unknown command %q (try: taskforce help)", args[0])
	}
}

func initCmd(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	path := fs.String("config", "", "config path")
	level := fs.String("level", string(config.LevelProject), "config level: profile, project, or workspace")
	repo := fs.String("repo", ".", "repository path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	target := *path
	if target == "" {
		paths, err := config.DiscoverPaths(*repo, "")
		if err != nil {
			return err
		}
		target, err = config.PathForLevel(paths, config.Level(*level))
		if err != nil {
			return err
		}
	}
	if _, err := os.Stat(target); err == nil {
		return fmt.Errorf("%s already exists", target)
	}
	return config.WriteLevelDefault(target)
}

func daemonCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: taskforce daemon start|status|stop|run [--repo PATH]")
	}
	action := args[0]
	fs := flag.NewFlagSet("daemon "+action, flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository path")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	absRepo, err := filepath.Abs(*repo)
	if err != nil {
		return err
	}
	switch action {
	case "start":
		state, err := daemon.Start(absRepo)
		if err != nil {
			return err
		}
		fmt.Println(daemon.Format(state, true))
	case "status":
		state, ok, err := daemon.Status()
		if err != nil {
			return err
		}
		fmt.Println(daemon.Format(state, ok))
	case "stop":
		if err := daemon.Stop(absRepo); err != nil {
			return err
		}
		fmt.Println("local daemon stopped")
	case "run":
		return daemon.Run(absRepo)
	default:
		return fmt.Errorf("unknown daemon command %q", action)
	}
	return nil
}

func configCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: taskforce config check|show|set|unset")
	}
	switch args[0] {
	case "check":
		fs := flag.NewFlagSet("config check", flag.ContinueOnError)
		path := fs.String("config", "", "config path")
		repo := fs.String("repo", ".", "repository path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		cfg, _, err := config.LoadEffective(*repo, *path)
		if err != nil {
			return err
		}
		if err := config.Validate(cfg); err != nil {
			return err
		}
		fmt.Println("config ok")
		return nil
	case "show":
		fs := flag.NewFlagSet("config show", flag.ContinueOnError)
		path := fs.String("config", "", "config path")
		level := fs.String("level", string(config.LevelEffective), "config level: effective, profile, project, or workspace")
		repo := fs.String("repo", ".", "repository path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		cfg, paths, err := config.LoadEffective(*repo, *path)
		if err != nil {
			return err
		}
		target := ""
		if config.Level(*level) != config.LevelEffective {
			target, err = config.PathForLevel(paths, config.Level(*level))
			if err != nil {
				return err
			}
		}
		data, err := config.Show(target, cfg)
		if err != nil {
			return err
		}
		fmt.Print(string(data))
		return nil
	case "set", "unset":
		fs := flag.NewFlagSet("config "+args[0], flag.ContinueOnError)
		level := fs.String("level", string(config.LevelWorkspace), "config level: profile, project, or workspace")
		repo := fs.String("repo", ".", "repository path")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		rest := fs.Args()
		need := 1
		if args[0] == "set" {
			need = 2
		}
		if len(rest) < need {
			return fmt.Errorf("usage: taskforce config %s --level LEVEL PATH [JSON_VALUE]", args[0])
		}
		paths, err := config.DiscoverPaths(*repo, "")
		if err != nil {
			return err
		}
		target, err := config.PathForLevel(paths, config.Level(*level))
		if err != nil {
			return err
		}
		if args[0] == "set" {
			return config.SetValue(target, rest[0], rest[1])
		}
		return config.UnsetValue(target, rest[0])
	default:
		return fmt.Errorf("unknown config command %q", args[0])
	}
}

func switchCmd(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: taskforce switch PATH")
	}
	resolved, err := workspace.Resolve(args[0])
	if err != nil {
		return err
	}
	state, _ := workspace.LoadState()
	state.ActiveRepo = resolved
	if err := workspace.SaveState(state); err != nil {
		return err
	}
	fmt.Printf("switched to %s\n", resolved)
	return nil
}

func usage() {
	fmt.Println(`TaskForce ` + version + ` — terminal command center for AI development pipelines

Usage:
  taskforce                                  open the live dashboard (resumes last active repo)
  taskforce switch PATH                      switch the active TaskForce target directory
  taskforce run --signal "..." [flags]       run the pipeline for a task
  taskforce run --signal-file PATH [flags]   run with a signal from a file
      flags: --repo PATH --config PATH --no-tui --yes --yolo --detach --local
  taskforce smoke [--no-tui]                 wiring test with echo commands
  taskforce runs [--limit N] [--json]        list recent pipeline runs
  taskforce runs show ID [--repo PATH]       print one run as JSON
  taskforce logs ID [--follow]               print streamed command output for a run
  taskforce approve ID [--reason "..."]      approve a run waiting at the release gate
  taskforce deny ID [--reason "..."]         deny a run waiting at the release gate
  taskforce agents                           list built-in and configured harnesses
  taskforce doctor                           check config, tools, and daemon health
  taskforce init [--level profile|project|workspace]
  taskforce config check|show|set|unset ...
  taskforce worktree add|list|remove [--repo PATH]
  taskforce daemon start|status|stop [--repo PATH]
  taskforce version`)
}
