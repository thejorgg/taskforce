// Package control runs the Relay planning stage through a configured harness.
package control

import (
	"context"
	"strings"
	"time"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/domain"
	"github.com/thejorgg/taskforce/internal/harness"
	"github.com/thejorgg/taskforce/internal/runner"
)

type Controller struct {
	Config config.StageConfig
	Agents map[string]config.AgentConfig
	Repo   string
	Runner runner.Runner
}

// Plan produces the execution plan for a task, running the configured
// planning harness when Control is enabled. Feedback from a prior failed
// attempt is appended so the planner can adjust.
func (c Controller) Plan(ctx context.Context, task domain.TaskPacket, feedback string) domain.ExecutionPlan {
	plan := domain.ExecutionPlan{
		Summary: "Plan implementation for " + task.Title,
		Steps: []string{
			"Inspect the repository and task packet.",
			"Identify files and commands required for the implementation.",
			"Return a plan suitable for the Build stage.",
		},
		Task:    task,
		Created: time.Now(),
	}
	if !c.Config.Enabled {
		plan.Summary = "Control disabled; using task packet as the implementation plan."
		return plan
	}
	spec, err := harness.Resolve(harness.Request{
		Stage:  "relay.control",
		Mode:   harness.ModePlan,
		Prompt: harness.RenderPrompt(c.Config.Prompt, task, feedback),
		Task:   task,
		Config: c.Config,
		Agents: c.Agents,
		Repo:   c.Repo,
	})
	if err != nil {
		plan.Summary = "Control unavailable: " + err.Error()
		plan.Meta = domain.StringTable{"error": err.Error()}
		return plan
	}
	plan.Command = &spec
	result := c.Runner.Run(ctx, spec)
	plan.Result = &result
	if strings.TrimSpace(result.Stdout) != "" {
		plan.Summary = strings.TrimSpace(result.Stdout)
	}
	return plan
}
