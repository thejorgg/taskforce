package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/daemon"
	"github.com/thejorgg/taskforce/internal/domain"
	"github.com/thejorgg/taskforce/internal/harness"
)

func agentsCmd(args []string) error {
	fs := flag.NewFlagSet("agents", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository path")
	cfgPath := fs.String("config", "", "explicit config override path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, _, err := config.LoadEffective(*repo, *cfgPath)
	if err != nil {
		return err
	}
	fmt.Println("built-in adapters:")
	for _, name := range harness.BuiltinNames() {
		fmt.Printf("  %-12s plan:  %s\n", name, sampleCommand(name, harness.ModePlan, cfg.Agents))
		fmt.Printf("  %-12s build: %s\n", "", sampleCommand(name, harness.ModeBuild, cfg.Agents))
	}
	if len(cfg.Agents) > 0 {
		fmt.Println("\nconfigured agents:")
		for _, name := range sortedAgentNames(cfg.Agents) {
			fmt.Printf("  %-12s plan:  %s\n", name, sampleCommand(name, harness.ModePlan, cfg.Agents))
			fmt.Printf("  %-12s build: %s\n", "", sampleCommand(name, harness.ModeBuild, cfg.Agents))
		}
	}
	fmt.Println("\nrelay assignment:")
	fmt.Printf("  control → %s%s\n", stageAgent(cfg.Relay.Control, harness.ModePlan), modelSuffix(cfg.Relay.Control.Model))
	fmt.Printf("  build   → %s%s\n", stageAgent(cfg.Relay.Build, harness.ModeBuild), modelSuffix(cfg.Relay.Build.Model))
	return nil
}

func sampleCommand(agent string, mode harness.Mode, agents map[string]config.AgentConfig) string {
	spec, err := harness.Resolve(harness.Request{
		Stage:  "relay.sample",
		Mode:   mode,
		Prompt: "{{prompt}}",
		Task:   domain.TaskPacket{ID: "{{task_id}}", Title: "{{task_title}}"},
		Config: config.StageConfig{Agent: agent},
		Agents: agents,
	})
	if err != nil {
		return "unavailable: " + err.Error()
	}
	if len(spec.Argv) > 0 {
		return strings.Join(spec.Argv, " ")
	}
	return spec.Run
}

func stageAgent(stage config.StageConfig, mode harness.Mode) string {
	if !stage.Enabled {
		return "disabled"
	}
	if stage.Run != "" || len(stage.Argv) > 0 {
		return "custom command"
	}
	if stage.Agent != "" {
		return stage.Agent
	}
	return harness.DefaultAgent(mode)
}

func modelSuffix(model string) string {
	if model == "" {
		return ""
	}
	return " (model: " + model + ")"
}

func sortedAgentNames(agents map[string]config.AgentConfig) []string {
	names := make([]string, 0, len(agents))
	for name := range agents {
		names = append(names, name)
	}
	for i := range names {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return names
}

func doctorCmd(args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	repo := fs.String("repo", ".", "repository path")
	cfgPath := fs.String("config", "", "explicit config override path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	absRepo, err := filepath.Abs(*repo)
	if err != nil {
		return err
	}
	failed := false
	report := func(level, name, detail string) {
		if level == "fail" {
			failed = true
		}
		fmt.Printf("%-5s %-22s %s\n", level, name, detail)
	}

	cfg, paths, err := config.LoadEffective(absRepo, *cfgPath)
	if err != nil {
		report("fail", "config", err.Error())
		fmt.Println("\ndoctor found problems")
		os.Exit(1)
	}
	report("ok", "config", "effective config loaded")
	if err := config.Validate(cfg); err != nil {
		report("fail", "config validate", err.Error())
	} else {
		report("ok", "config validate", "no errors")
	}
	for _, item := range []struct{ name, path string }{
		{"profile config", paths.Profile},
		{"project config", paths.Project},
		{"workspace config", paths.Workspace},
	} {
		if item.path == "" {
			continue
		}
		if _, err := os.Stat(item.path); err == nil {
			report("ok", item.name, item.path)
		} else {
			report("info", item.name, item.path+" (not present)")
		}
	}

	if _, err := os.Stat(filepath.Join(absRepo, ".git")); err == nil {
		report("ok", "git repository", absRepo)
	} else {
		report("warn", "git repository", "no .git found; exfil branch/commit/push will fail")
	}
	checkBinary(report, "git", "git", cfg.Exfil.Commit || cfg.Exfil.Push || cfg.Exfil.Branch != "")
	if cfg.Exfil.PR {
		checkBinary(report, "gh (exfil.pr)", "gh", true)
	}

	checkStageBinary(report, "relay.control", cfg.Relay.Control, harness.ModePlan, cfg.Agents)
	checkStageBinary(report, "relay.build", cfg.Relay.Build, harness.ModeBuild, cfg.Agents)
	for _, hook := range cfg.Scope.Hooks {
		checkHookBinary(report, "scope."+hook.Name, hook)
	}
	for _, hook := range cfg.Exfil.Hooks {
		checkHookBinary(report, "exfil."+hook.Name, hook)
	}

	state, ok, err := daemon.Status(absRepo)
	switch {
	case err != nil:
		report("warn", "daemon", err.Error())
	case ok && state.Status == "running":
		report("ok", "daemon", daemon.Format(state, ok))
	default:
		report("info", "daemon", "not running · starts automatically with the dashboard or runs")
	}

	if failed {
		fmt.Println("\ndoctor found problems")
		os.Exit(1)
	}
	fmt.Println("\nall required checks passed")
	return nil
}

func checkStageBinary(report func(level, name, detail string), name string, stage config.StageConfig, mode harness.Mode, agents map[string]config.AgentConfig) {
	if !stage.Enabled {
		report("info", name, "disabled")
		return
	}
	spec, err := harness.Resolve(harness.Request{
		Stage: name, Mode: mode, Prompt: "doctor", Task: domain.TaskPacket{ID: "doctor"},
		Config: stage, Agents: agents,
	})
	if err != nil {
		report("fail", name, err.Error())
		return
	}
	binary := commandBinary(spec.Run, spec.Argv)
	if binary == "" {
		report("warn", name, "no command resolved")
		return
	}
	if _, err := exec.LookPath(binary); err != nil {
		report("warn", name, binary+" not found on PATH")
		return
	}
	report("ok", name, binary+" available")
}

func checkHookBinary(report func(level, name, detail string), name string, hook config.HookConfig) {
	binary := commandBinary(hook.Run, hook.Argv)
	if binary == "" {
		report("warn", name, "no command configured")
		return
	}
	if _, err := exec.LookPath(binary); err != nil {
		report("warn", name, binary+" not found on PATH")
		return
	}
	report("ok", name, binary+" available")
}

func checkBinary(report func(level, name, detail string), name, binary string, required bool) {
	if _, err := exec.LookPath(binary); err != nil {
		level := "warn"
		if required {
			level = "fail"
		}
		report(level, name, binary+" not found on PATH")
		return
	}
	report("ok", name, binary+" available")
}

func commandBinary(run string, argv []string) string {
	if len(argv) > 0 {
		return argv[0]
	}
	fields := strings.Fields(run)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
