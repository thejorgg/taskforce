// Package build runs the Relay implementation stage through a configured harness.
package build

import (
	"context"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/domain"
	"github.com/thejorgg/taskforce/internal/harness"
	"github.com/thejorgg/taskforce/internal/runner"
)

type Builder struct {
	Config config.StageConfig
	Agents map[string]config.AgentConfig
	Repo   string
	Runner runner.Runner
}

// Execute runs the configured build harness against the plan. Attempt is
// 1-based and recorded on the result so retries are visible downstream.
func (b Builder) Execute(ctx context.Context, task domain.TaskPacket, plan domain.ExecutionPlan, attempt int) domain.RelayResult {
	if attempt < 1 {
		attempt = 1
	}
	result := domain.RelayResult{Plan: plan, Attempts: attempt}
	if !b.Config.Enabled {
		result.Feedback = "Build disabled."
		return result
	}
	spec, err := harness.Resolve(harness.Request{
		Stage:  "relay.build",
		Mode:   harness.ModeBuild,
		Prompt: harness.RenderPrompt(b.Config.Prompt, task, plan.Summary),
		Task:   task,
		Config: b.Config,
		Agents: b.Agents,
		Repo:   b.Repo,
	})
	if err != nil {
		result.Feedback = err.Error()
		return result
	}
	buildResult := b.Runner.Run(ctx, spec)
	result.BuildResult = &buildResult
	result.Approved = buildResult.ExitCode == 0 && !buildResult.Skipped
	if buildResult.ExitCode != 0 {
		result.Feedback = buildResult.Error
		if out := buildResult.Output(); out != "" {
			result.Feedback = out
		}
	}
	if buildResult.Skipped {
		result.Feedback = buildResult.Stdout
	}
	return result
}
