// Package harness resolves the command for a Relay stage from a pluggable
// adapter registry: explicit stage run/argv overrides, custom agents defined
// in config, or built-in adapters for well-known coding agents.
package harness

import (
	"fmt"
	"sort"
	"strings"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/domain"
)

// Mode selects the read-only planning command or the mutating build command.
type Mode string

const (
	ModePlan  Mode = "plan"
	ModeBuild Mode = "build"
)

// Request carries everything needed to resolve a stage command.
type Request struct {
	Stage  string
	Mode   Mode
	Prompt string
	Task   domain.TaskPacket
	Config config.StageConfig
	Agents map[string]config.AgentConfig
	Repo   string
}

type builtin struct {
	plan  func(model, prompt string) []string
	build func(model, prompt string) []string
}

var builtins = map[string]builtin{
	"claude": {
		plan: func(model, prompt string) []string {
			return withModelFlag([]string{"claude", "-p", prompt, "--permission-mode", "plan", "--output-format", "text"}, "--model", model)
		},
		build: func(model, prompt string) []string {
			return withModelFlag([]string{"claude", "-p", prompt, "--permission-mode", "acceptEdits", "--output-format", "text"}, "--model", model)
		},
	},
	"codex": {
		plan: func(model, prompt string) []string {
			argv := withModelFlag([]string{"codex", "exec", "--skip-git-repo-check", "--sandbox", "read-only"}, "--model", model)
			return append(argv, prompt)
		},
		build: func(model, prompt string) []string {
			argv := withModelFlag([]string{"codex", "exec", "--skip-git-repo-check", "--sandbox", "workspace-write"}, "--model", model)
			return append(argv, prompt)
		},
	},
	"opencode": {
		plan: func(model, prompt string) []string {
			argv := withModelFlag([]string{"opencode", "run"}, "--model", model)
			return append(argv, prompt)
		},
		build: func(model, prompt string) []string {
			argv := withModelFlag([]string{"opencode", "run"}, "--model", model)
			return append(argv, prompt)
		},
	},
	"gemini": {
		plan: func(model, prompt string) []string {
			return withModelFlag([]string{"gemini", "-p", prompt}, "-m", model)
		},
		build: func(model, prompt string) []string {
			return withModelFlag([]string{"gemini", "--yolo", "-p", prompt}, "-m", model)
		},
	},
}

func withModelFlag(argv []string, flag, model string) []string {
	if model != "" {
		return append(argv, flag, model)
	}
	return argv
}

// BuiltinNames lists built-in adapters in stable order.
func BuiltinNames() []string {
	names := make([]string, 0, len(builtins))
	for name := range builtins {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// DefaultAgent is the adapter used when a stage leaves agent empty.
func DefaultAgent(mode Mode) string {
	if mode == ModeBuild {
		return "opencode"
	}
	return "codex"
}

// Resolve produces the command spec for a stage. Resolution order: stage
// run/argv override, then a custom agent from config, then a built-in
// adapter. It returns an error when the named agent is unknown.
func Resolve(req Request) (domain.CommandSpec, error) {
	mutates := req.Mode == ModeBuild || req.Config.Mutates
	spec := domain.CommandSpec{
		Name:     req.Stage,
		Env:      stageEnv(req),
		WorkDir:  expand(req.Config.WorkDir, placeholderVars(req)),
		Timeout:  req.Config.Timeout,
		Required: true,
		Mutates:  mutates,
	}
	vars := placeholderVars(req)
	if req.Config.Run != "" || len(req.Config.Argv) > 0 {
		spec.Run = expand(req.Config.Run, vars)
		spec.Argv = expandArgv(req.Config.Argv, vars)
		mergeEnv(spec.Env, req.Config.Env, vars)
		return spec, nil
	}
	agent := strings.TrimSpace(req.Config.Agent)
	if agent == "" {
		agent = DefaultAgent(req.Mode)
	}
	if custom, ok := req.Agents[agent]; ok {
		run, argv := customCommand(custom, req.Mode)
		spec.Run = expand(run, vars)
		spec.Argv = expandArgv(argv, vars)
		mergeEnv(spec.Env, custom.Env, vars)
		mergeEnv(spec.Env, req.Config.Env, vars)
		if spec.Timeout == "" {
			spec.Timeout = custom.Timeout
		}
		return spec, nil
	}
	adapter, ok := builtins[agent]
	if !ok {
		return domain.CommandSpec{}, fmt.Errorf("no agent %q: define agents.%s in config, or set %s.run or %s.argv", agent, agent, req.Stage, req.Stage)
	}
	if req.Mode == ModeBuild {
		spec.Argv = adapter.build(req.Config.Model, req.Prompt)
	} else {
		spec.Argv = adapter.plan(req.Config.Model, req.Prompt)
	}
	mergeEnv(spec.Env, req.Config.Env, vars)
	return spec, nil
}

func customCommand(agent config.AgentConfig, mode Mode) (string, []string) {
	if mode == ModeBuild && agent.Build != nil {
		return agent.Build.Run, agent.Build.Argv
	}
	if mode == ModePlan && agent.Plan != nil {
		return agent.Plan.Run, agent.Plan.Argv
	}
	return agent.Run, agent.Argv
}

func stageEnv(req Request) map[string]string {
	return map[string]string{
		"TASKFORCE_TASK_ID":          req.Task.ID,
		"TASKFORCE_TASK_TITLE":       req.Task.Title,
		"TASKFORCE_TASK_DESCRIPTION": req.Task.Description,
		"TASKFORCE_STAGE":            req.Stage,
		"TASKFORCE_MODE":             string(req.Mode),
		"TASKFORCE_PROMPT":           req.Prompt,
		"TASKFORCE_MODEL":            req.Config.Model,
		"TASKFORCE_REPO":             req.Repo,
	}
}

func placeholderVars(req Request) map[string]string {
	return map[string]string{
		"prompt":           req.Prompt,
		"model":            req.Config.Model,
		"task_id":          req.Task.ID,
		"task_title":       req.Task.Title,
		"task_description": req.Task.Description,
		"repo":             req.Repo,
		"mode":             string(req.Mode),
	}
}

func expandArgv(argv []string, vars map[string]string) []string {
	if len(argv) == 0 {
		return nil
	}
	out := make([]string, len(argv))
	for i, arg := range argv {
		out[i] = expand(arg, vars)
	}
	return out
}

func expand(value string, vars map[string]string) string {
	for key, replacement := range vars {
		value = strings.ReplaceAll(value, "{{"+key+"}}", replacement)
	}
	return value
}

// mergeEnv copies src into dst, expanding {{placeholders}} in the values.
func mergeEnv(dst, src map[string]string, vars map[string]string) {
	for k, v := range src {
		dst[k] = expand(v, vars)
	}
}

// RenderPrompt combines the configured stage prompt with task context and
// any upstream plan output.
func RenderPrompt(base string, task domain.TaskPacket, plan string) string {
	if strings.TrimSpace(base) == "" {
		base = "Process this TaskForce task."
	}
	return fmt.Sprintf("%s\n\nTask ID: %s\nTitle: %s\nDescription:\n%s\n\nPlan:\n%s", base, task.ID, task.Title, task.Description, plan)
}
