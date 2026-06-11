package exfil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/domain"
	"github.com/thejorgg/taskforce/internal/merge"
	"github.com/thejorgg/taskforce/internal/runner"
)

type Releaser struct {
	Config       config.ExfilConfig
	Runner       runner.Runner
	RecentMerges []string
}

func (r Releaser) Release(ctx context.Context, task domain.TaskPacket, review domain.ReviewResult) domain.ReleaseResult {
	if review.Status != domain.ReviewApproved {
		return domain.ReleaseResult{Skipped: true}
	}
	if len(r.RecentMerges) == 0 {
		r.RecentMerges = merge.LoadRecentMerges()
	}
	out := domain.ReleaseResult{}
	branch := render(r.Config.Branch, task)
	if branch != "" {
		res := r.Runner.Run(ctx, domain.CommandSpec{Name: "exfil.branch", Argv: []string{"git", "checkout", "-B", branch}, Mutates: true, Required: true})
		out.Results = append(out.Results, res)
		out.Branch = branch
		if res.ExitCode != 0 {
			out.Skipped = true
			return out
		}
	}
	for _, hook := range r.Config.Hooks {
		res := r.Runner.Run(ctx, domain.CommandSpec{Name: "exfil." + hook.Name, Run: hook.Run, Argv: hook.Argv, Env: hook.Env, WorkDir: hook.WorkDir, Timeout: hook.Timeout, Mutates: true, Required: hook.Required})
		out.Results = append(out.Results, res)
		if hook.Required && res.ExitCode != 0 {
			out.Skipped = true
			return out
		}
	}
	if r.Config.Commit {
		add := r.Runner.Run(ctx, domain.CommandSpec{Name: "exfil.git_add", Argv: []string{"git", "add", "-A"}, Mutates: true, Required: true})
		out.Results = append(out.Results, add)
		if add.ExitCode != 0 {
			out.Skipped = true
			return out
		}
		msg := render(r.Config.CommitMessage, task)
		if msg == "" {
			msg = "TaskForce: " + task.Title
		}
		commit := r.Runner.Run(ctx, domain.CommandSpec{Name: "exfil.git_commit", Argv: []string{"git", "commit", "-m", msg}, Mutates: true, Required: true})
		out.Results = append(out.Results, commit)
		out.Commit = strings.TrimSpace(commit.Stdout)
		if commit.ExitCode != 0 {
			out.Skipped = true
			return out
		}
	}
	if r.Config.Push {
		if warning := merge.CheckMergeWarning(branch, r.RecentMerges); warning != nil {
			fmt.Fprintln(os.Stderr, warning.Message)
		}
		target := "HEAD"
		if branch != "" {
			target = branch
		}
		push := r.Runner.Run(ctx, domain.CommandSpec{Name: "exfil.git_push", Argv: []string{"git", "push", "-u", "origin", target}, Mutates: true, Required: true})
		out.Results = append(out.Results, push)
		out.Pushed = push.ExitCode == 0
		if push.ExitCode != 0 {
			out.Skipped = true
			return out
		}
	}
	if r.Config.PR {
		if warning := merge.CheckMergeWarning(branch, r.RecentMerges); warning != nil {
			fmt.Fprintln(os.Stderr, warning.Message)
		}
		if _, err := exec.LookPath("gh"); err != nil {
			out.Results = append(out.Results, domain.CommandResult{Name: "exfil.pr", ExitCode: 127, Error: "gh not found; install GitHub CLI or disable exfil.pr"})
			out.Skipped = true
			return out
		}
		pr := r.Runner.Run(ctx, domain.CommandSpec{
			Name:    "exfil.pr",
			Argv:    []string{"gh", "pr", "create", "--title", render(r.Config.PRTitle, task), "--body", render(r.Config.PRBody, task)},
			Mutates: true, Required: true,
		})
		out.Results = append(out.Results, pr)
		out.PRURL = strings.TrimSpace(pr.Stdout)
		if pr.ExitCode != 0 {
			out.Skipped = true
		}
	}
	return out
}

func render(template string, task domain.TaskPacket) string {
	replacer := strings.NewReplacer(
		"{{task_id}}", task.ID,
		"{{task_title}}", task.Title,
		"{{category}}", task.Category,
	)
	return strings.TrimSpace(replacer.Replace(template))
}

func Describe(result domain.ReleaseResult) string {
	if result.Skipped {
		return "Exfil skipped or stopped before completion."
	}
	parts := []string{}
	if result.Branch != "" {
		parts = append(parts, "branch "+result.Branch)
	}
	if result.Pushed {
		parts = append(parts, "pushed")
	}
	if result.PRURL != "" {
		parts = append(parts, "PR "+result.PRURL)
	}
	if len(parts) == 0 {
		return "No Exfil actions configured."
	}
	return fmt.Sprintf("Exfil complete: %s.", strings.Join(parts, ", "))
}
