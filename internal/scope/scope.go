package scope

import (
	"context"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/domain"
	"github.com/thejorgg/taskforce/internal/runner"
)

type Reviewer struct {
	Config config.ScopeConfig
	Runner runner.Runner
}

func (r Reviewer) Review(ctx context.Context, task domain.TaskPacket, relay domain.RelayResult) domain.ReviewResult {
	if relay.BuildResult != nil && relay.BuildResult.ExitCode != 0 {
		return domain.ReviewResult{Status: domain.ReviewNeedsRevision, Reason: "Relay Build failed.", Feedback: []string{relay.BuildResult.Output()}}
	}
	if len(r.Config.Hooks) == 0 {
		return domain.ReviewResult{Status: domain.ReviewApproved, Reason: "No Scope hooks configured."}
	}
	results := make([]domain.CommandResult, 0, len(r.Config.Hooks))
	for _, hook := range r.Config.Hooks {
		spec := domain.CommandSpec{
			Name:     "scope." + hook.Name,
			Run:      hook.Run,
			Argv:     hook.Argv,
			Env:      hook.Env,
			WorkDir:  hook.WorkDir,
			Timeout:  hook.Timeout,
			Required: hook.Required,
			Mutates:  false,
		}
		result := r.Runner.Run(ctx, spec)
		results = append(results, result)
		if hook.Required && result.ExitCode != 0 {
			return domain.ReviewResult{Status: domain.ReviewNeedsRevision, Reason: "required Scope hook failed: " + hook.Name, Hooks: results, Feedback: []string{result.Output()}}
		}
	}
	return domain.ReviewResult{Status: domain.ReviewApproved, Reason: "Relay output passed configured Scope checks.", Hooks: results}
}
