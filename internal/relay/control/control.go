package control

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/domain"
	"github.com/thejorgg/taskforce/internal/runner"
)

type Controller struct {
	Config config.StageConfig
	Runner runner.Runner
}

func (c Controller) Plan(ctx context.Context, task domain.TaskPacket) domain.ExecutionPlan {
	spec := c.command(task)
	plan := domain.ExecutionPlan{
		Summary: fmt.Sprintf("Plan implementation for %s", task.Title),
		Steps: []string{
			"Inspect the repository and task packet.",
			"Identify files and commands required for the implementation.",
			"Return a plan suitable for the Build stage.",
		},
		Command: &spec,
		Task:    task,
		Created: time.Now(),
	}
	if !c.Config.Enabled {
		plan.Summary = "Control disabled; using task packet as the implementation plan."
		return plan
	}
	result := c.Runner.Run(ctx, spec)
	plan.Result = &result
	if strings.TrimSpace(result.Stdout) != "" {
		plan.Summary = strings.TrimSpace(result.Stdout)
	}
	return plan
}

func (c Controller) command(task domain.TaskPacket) domain.CommandSpec {
	name := "relay.control"
	env := taskEnv(task)
	for k, v := range c.Config.Env {
		env[k] = v
	}
	if c.Config.Run != "" || len(c.Config.Argv) > 0 {
		return domain.CommandSpec{Name: name, Run: c.Config.Run, Argv: c.Config.Argv, Env: env, Timeout: c.Config.Timeout, Required: true, Mutates: c.Config.Mutates}
	}
	prompt := renderPrompt(c.Config.Prompt, task, "")
	switch c.Config.Agent {
	case "claude":
		return domain.CommandSpec{Name: name, Argv: []string{"claude", "-p", prompt, "--permission-mode", "plan", "--output-format", "text"}, Env: env, Timeout: c.Config.Timeout, Required: true}
	case "codex", "":
		return domain.CommandSpec{Name: name, Argv: []string{"codex", "exec", "--skip-git-repo-check", "--sandbox", "read-only", prompt}, Env: env, Timeout: c.Config.Timeout, Required: true}
	default:
		return domain.CommandSpec{Name: name, Run: fmt.Sprintf("echo TaskForce: no built-in Control adapter for %q. Set relay.control.run or relay.control.argv.", c.Config.Agent), Env: env}
	}
}

func taskEnv(task domain.TaskPacket) map[string]string {
	return map[string]string{
		"TASKFORCE_TASK_ID":          task.ID,
		"TASKFORCE_TASK_TITLE":       task.Title,
		"TASKFORCE_TASK_DESCRIPTION": task.Description,
		"TASKFORCE_STAGE":            "relay.control",
	}
}

func renderPrompt(base string, task domain.TaskPacket, plan string) string {
	if strings.TrimSpace(base) == "" {
		base = "Process this TaskForce task."
	}
	return fmt.Sprintf("%s\n\nTask ID: %s\nTitle: %s\nDescription:\n%s\n\nPrior plan/output:\n%s", base, task.ID, task.Title, task.Description, plan)
}
