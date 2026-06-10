package build

import (
	"context"
	"fmt"
	"strings"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/domain"
	"github.com/thejorgg/taskforce/internal/runner"
)

type Builder struct {
	Config config.StageConfig
	Runner runner.Runner
}

func (b Builder) Execute(ctx context.Context, task domain.TaskPacket, plan domain.ExecutionPlan) domain.RelayResult {
	result := domain.RelayResult{Plan: plan, Attempts: 1}
	if !b.Config.Enabled {
		result.Feedback = "Build disabled."
		return result
	}
	spec := b.command(task, plan)
	buildResult := b.Runner.Run(ctx, spec)
	result.BuildResult = &buildResult
	result.Approved = buildResult.ExitCode == 0 && !buildResult.Skipped
	if buildResult.ExitCode != 0 {
		result.Feedback = buildResult.Error
	}
	if buildResult.Skipped {
		result.Feedback = buildResult.Stdout
	}
	return result
}

func (b Builder) command(task domain.TaskPacket, plan domain.ExecutionPlan) domain.CommandSpec {
	name := "relay.build"
	env := map[string]string{
		"TASKFORCE_TASK_ID":          task.ID,
		"TASKFORCE_TASK_TITLE":       task.Title,
		"TASKFORCE_TASK_DESCRIPTION": task.Description,
		"TASKFORCE_STAGE":            "relay.build",
	}
	for k, v := range b.Config.Env {
		env[k] = v
	}
	if b.Config.Run != "" || len(b.Config.Argv) > 0 {
		return domain.CommandSpec{Name: name, Run: b.Config.Run, Argv: b.Config.Argv, Env: env, Timeout: b.Config.Timeout, Required: true, Mutates: true}
	}
	prompt := renderPrompt(b.Config.Prompt, task, plan.Summary)
	switch b.Config.Agent {
	case "opencode", "":
		return domain.CommandSpec{Name: name, Argv: []string{"opencode", "run", prompt}, Env: env, Timeout: b.Config.Timeout, Required: true, Mutates: true}
	case "codex":
		return domain.CommandSpec{Name: name, Argv: []string{"codex", "exec", "--skip-git-repo-check", "--sandbox", "workspace-write", prompt}, Env: env, Timeout: b.Config.Timeout, Required: true, Mutates: true}
	default:
		return domain.CommandSpec{Name: name, Run: fmt.Sprintf("echo TaskForce: no built-in Build adapter for %q. Set relay.build.run or relay.build.argv.", b.Config.Agent), Env: env}
	}
}

func renderPrompt(base string, task domain.TaskPacket, plan string) string {
	if strings.TrimSpace(base) == "" {
		base = "Implement this TaskForce task."
	}
	return fmt.Sprintf("%s\n\nTask ID: %s\nTitle: %s\nDescription:\n%s\n\nPlan:\n%s", base, task.ID, task.Title, task.Description, plan)
}
