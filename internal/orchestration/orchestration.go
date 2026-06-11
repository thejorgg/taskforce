// Package orchestration drives a task through the Echo, Dispatch, Relay,
// Scope, and Exfil stages, reporting progress through an observer callback.
package orchestration

import (
	"context"
	"fmt"
	"time"

	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/dispatch"
	"github.com/thejorgg/taskforce/internal/domain"
	"github.com/thejorgg/taskforce/internal/echo"
	"github.com/thejorgg/taskforce/internal/exfil"
	"github.com/thejorgg/taskforce/internal/harness"
	"github.com/thejorgg/taskforce/internal/relay/build"
	"github.com/thejorgg/taskforce/internal/relay/control"
	"github.com/thejorgg/taskforce/internal/rescue"
	"github.com/thejorgg/taskforce/internal/runner"
	"github.com/thejorgg/taskforce/internal/scope"
)

type Options struct {
	Config config.Config
	Repo   string
	Runner runner.Runner
	// RunID overrides the generated pipeline run ID so external trackers
	// (such as the daemon run queue) can correlate progress.
	RunID string
	// Observe receives a snapshot of the run after every stage transition.
	Observe func(domain.PipelineRun)
}

type Pipeline struct {
	Options Options
	stages  []domain.StageSnapshot
}

func New(opts Options) *Pipeline {
	return &Pipeline{Options: opts, stages: initialStages()}
}

func (p *Pipeline) RunText(ctx context.Context, source, text string) domain.PipelineRun {
	start := time.Now()
	id := p.Options.RunID
	if id == "" {
		id = fmt.Sprintf("run-%d", start.UnixNano())
	}
	run := domain.PipelineRun{ID: id, Repo: p.Options.Repo, StartedAt: start}

	p.set(domain.StageEcho, domain.StatusRunning, "normalizing signal")
	p.observe(&run)
	run.Signal = echo.Collector{}.FromText(source, text, nil)
	p.set(domain.StageEcho, domain.StatusPassed, "signal "+run.Signal.ID)
	p.observe(&run)

	p.set(domain.StageDispatch, domain.StatusRunning, "triaging signal")
	p.observe(&run)
	run.Task = dispatch.Dispatcher{}.Dispatch(run.Signal)
	if !run.Task.Actionable {
		p.set(domain.StageDispatch, domain.StatusFailed, "signal is not actionable")
		return p.finish(&run)
	}
	p.set(domain.StageDispatch, domain.StatusPassed, "task "+run.Task.ID+" · "+run.Task.Category+" · p"+fmt.Sprint(run.Task.Priority))
	p.observe(&run)

	p.set(domain.StageRelay, domain.StatusRunning, "running Control and Build")
	p.observe(&run)
	controller := control.Controller{Config: p.Options.Config.Relay.Control, Agents: p.Options.Config.Agents, Repo: p.Options.Repo, Runner: p.Options.Runner}
	builder := build.Builder{Config: p.Options.Config.Relay.Build, Agents: p.Options.Config.Agents, Repo: p.Options.Repo, Runner: p.Options.Runner}
	rescueTeam := rescue.Team{Config: p.Options.Config.Rescue, Agents: p.Options.Config.Agents, Repo: p.Options.Repo, Runner: p.Options.Runner}
	attempts := p.Options.Config.Relay.Retries + 1
	if attempts < 1 {
		attempts = 1
	}
	feedback := ""
	for attempt := 1; attempt <= attempts; attempt++ {
		if attempt > 1 {
			p.append(domain.StageRelay, fmt.Sprintf("attempt %d/%d · retrying with feedback", attempt, attempts))
			p.observe(&run)
		}
		persistedMappings, rescueActive, rescueStateErr := rescue.ActiveMappings(p.Options.Repo)
		if rescueStateErr != nil {
			p.append(domain.StageRelay, "rescue state unavailable: "+rescueStateErr.Error())
			p.observe(&run)
		}
		var plan domain.ExecutionPlan
		if rescueActive {
			p.append(domain.StageRelay, "rescue mappings active for this repo; running relay.control in neutral temporary repo")
			p.observe(&run)
			plan = domain.ExecutionPlan{
				Summary: "Plan implementation for " + run.Task.Title,
				Steps: []string{
					"Inspect the repository and task packet.",
					"Identify files and commands required for the implementation.",
					"Return a plan suitable for the Build stage.",
				},
				Task:    run.Task,
				Created: time.Now(),
			}
			if !p.Options.Config.Relay.Control.Enabled {
				plan.Summary = "Control disabled; using task packet as the implementation plan."
			} else {
				controlPrompt := harness.RenderPrompt(p.Options.Config.Relay.Control.Prompt, run.Task, feedback)
				mapped, err := rescueTeam.RunStage(ctx, "relay.control.mapped", harness.ModePlan, run.Task, controlPrompt, p.Options.Config.Relay.Control, persistedMappings)
				if err != nil {
					p.append(domain.StageRelay, "mapped relay.control failed: "+err.Error())
					p.observe(&run)
					plan.Summary = "Control unavailable: " + err.Error()
					plan.Meta = domain.StringTable{"error": err.Error()}
				} else {
					p.append(domain.StageRelay, "mapped repo: "+mapped.TempRepo)
					p.append(domain.StageRelay, fmt.Sprintf("mapped attempts: %d", mapped.Attempts))
					p.append(domain.StageRelay, mapped.Run.Output())
					p.observe(&run)
					plan.Command = &mapped.Command
					plan.Result = &mapped.Run
					if mapped.Run.ExitCode == 0 && !mapped.Run.Skipped && mapped.Run.Stdout != "" {
						plan.Summary = mapped.Run.Stdout
					}
				}
			}
		} else {
			plan = controller.Plan(ctx, run.Task, feedback)
		}
		if plan.Result != nil {
			p.append(domain.StageRelay, plan.Result.Output())
			p.observe(&run)
			if rescue.Triggered(p.Options.Config.Rescue, plan.Result) {
				p.append(domain.StageRelay, "rescue triggered for relay.control; preparing neutral temporary repo")
				p.observe(&run)
				rescuePrompt := harness.RenderPrompt(p.Options.Config.Relay.Control.Prompt, run.Task, feedback)
				rescueResult, err := rescueTeam.Run(ctx, "relay.control", harness.ModePlan, run.Task, rescuePrompt)
				if err != nil {
					p.append(domain.StageRelay, "rescue failed: "+err.Error())
					p.observe(&run)
				} else {
					p.append(domain.StageRelay, "rescue repo: "+rescueResult.TempRepo)
					p.append(domain.StageRelay, fmt.Sprintf("rescue attempts: %d", rescueResult.Attempts))
					p.append(domain.StageRelay, rescueResult.Run.Output())
					p.observe(&run)
					plan.Command = &rescueResult.Command
					plan.Result = &rescueResult.Run
					if rescueResult.Run.ExitCode == 0 && !rescueResult.Run.Skipped && rescueResult.Run.Stdout != "" {
						plan.Summary = rescueResult.Run.Stdout
					}
				}
			}
		}
		if rescueActive {
			p.append(domain.StageRelay, "rescue mappings active for this repo; running relay.build in neutral temporary repo")
			p.observe(&run)
			run.Relay = domain.RelayResult{Plan: plan, Attempts: attempt}
			if !p.Options.Config.Relay.Build.Enabled {
				run.Relay.Feedback = "Build disabled."
			} else {
				buildPrompt := harness.RenderPrompt(p.Options.Config.Relay.Build.Prompt, run.Task, plan.Summary)
				mapped, err := rescueTeam.RunStage(ctx, "relay.build.mapped", harness.ModeBuild, run.Task, buildPrompt, p.Options.Config.Relay.Build, persistedMappings)
				if err != nil {
					run.Relay.Feedback = "mapped relay.build failed: " + err.Error()
					p.append(domain.StageRelay, run.Relay.Feedback)
					p.observe(&run)
				} else {
					p.append(domain.StageRelay, "mapped repo: "+mapped.TempRepo)
					p.append(domain.StageRelay, fmt.Sprintf("mapped attempts: %d", mapped.Attempts))
					p.append(domain.StageRelay, mapped.Run.Output())
					p.observe(&run)
					run.Relay.BuildResult = &mapped.Run
					run.Relay.Approved = mapped.Run.ExitCode == 0 && !mapped.Run.Skipped
					if mapped.Run.ExitCode != 0 {
						run.Relay.Feedback = mapped.Run.Error
						if out := mapped.Run.Output(); out != "" {
							run.Relay.Feedback = out
						}
					}
					if mapped.Run.Skipped {
						run.Relay.Feedback = mapped.Run.Stdout
					}
				}
			}
		} else {
			run.Relay = builder.Execute(ctx, run.Task, plan, attempt)
		}
		if run.Relay.BuildResult != nil {
			p.append(domain.StageRelay, run.Relay.BuildResult.Output())
			p.observe(&run)
			if rescue.Triggered(p.Options.Config.Rescue, run.Relay.BuildResult) {
				p.append(domain.StageRelay, "rescue triggered for relay.build; preparing neutral temporary repo")
				p.observe(&run)
				rescuePrompt := harness.RenderPrompt(p.Options.Config.Relay.Build.Prompt, run.Task, plan.Summary)
				rescueResult, err := rescueTeam.Run(ctx, "relay.build", harness.ModeBuild, run.Task, rescuePrompt)
				if err != nil {
					run.Relay.Feedback = "rescue failed: " + err.Error()
					p.append(domain.StageRelay, run.Relay.Feedback)
					p.observe(&run)
				} else {
					p.append(domain.StageRelay, "rescue repo: "+rescueResult.TempRepo)
					p.append(domain.StageRelay, fmt.Sprintf("rescue attempts: %d", rescueResult.Attempts))
					p.append(domain.StageRelay, rescueResult.Run.Output())
					p.observe(&run)
					run.Relay.BuildResult = &rescueResult.Run
					run.Relay.Approved = rescueResult.Run.ExitCode == 0 && !rescueResult.Run.Skipped
					if run.Relay.Approved {
						run.Relay.Feedback = "rescue completed and mapped changes back before Scope"
					} else if out := rescueResult.Run.Output(); out != "" {
						run.Relay.Feedback = out
					}
				}
			}
		}
		if run.Relay.Approved {
			break
		}
		if run.Relay.BuildResult != nil && run.Relay.BuildResult.Skipped {
			break
		}
		feedback = run.Relay.Feedback
		if ctx.Err() != nil {
			break
		}
	}
	if !run.Relay.Approved {
		p.set(domain.StageRelay, domain.StatusFailed, run.Relay.Feedback)
		return p.finish(&run)
	}
	p.set(domain.StageRelay, domain.StatusPassed, run.Relay.Feedback)
	p.observe(&run)

	p.set(domain.StageScope, domain.StatusRunning, "running Scope hooks")
	p.observe(&run)
	reviewer := scope.Reviewer{Config: p.Options.Config.Scope, Runner: p.Options.Runner}
	run.Review = reviewer.Review(ctx, run.Task, run.Relay)
	p.append(domain.StageScope, run.Review.Reason)
	for _, hook := range run.Review.Hooks {
		p.append(domain.StageScope, hook.Output())
	}
	if run.Review.Status != domain.ReviewApproved {
		p.set(domain.StageScope, domain.StatusNeedsRevision, run.Review.Reason)
		return p.finish(&run)
	}
	p.set(domain.StageScope, domain.StatusPassed, "")
	p.observe(&run)

	p.set(domain.StageExfil, domain.StatusRunning, "running Exfil")
	p.observe(&run)
	releaser := exfil.Releaser{Config: p.Options.Config.Exfil, Runner: p.Options.Runner}
	run.Release = releaser.Release(ctx, run.Task, run.Review)
	p.append(domain.StageExfil, exfil.Describe(run.Release))
	if run.Release.Skipped {
		p.set(domain.StageExfil, domain.StatusSkipped, "Exfil skipped or stopped")
	} else {
		p.set(domain.StageExfil, domain.StatusPassed, "Exfil complete")
	}
	return p.finish(&run)
}

func (p *Pipeline) finish(run *domain.PipelineRun) domain.PipelineRun {
	run.EndedAt = time.Now()
	p.observe(run)
	return *run
}

func initialStages() []domain.StageSnapshot {
	return []domain.StageSnapshot{
		{Name: domain.StageEcho, Status: domain.StatusIdle},
		{Name: domain.StageDispatch, Status: domain.StatusIdle},
		{Name: domain.StageRelay, Status: domain.StatusIdle},
		{Name: domain.StageScope, Status: domain.StatusIdle},
		{Name: domain.StageExfil, Status: domain.StatusIdle},
	}
}

func (p *Pipeline) observe(run *domain.PipelineRun) {
	run.Stages = snapshotStages(p.stages)
	if p.Options.Observe != nil {
		p.Options.Observe(*run)
	}
}

func snapshotStages(stages []domain.StageSnapshot) []domain.StageSnapshot {
	out := make([]domain.StageSnapshot, len(stages))
	for i, stage := range stages {
		out[i] = stage
		out[i].Logs = append([]string(nil), stage.Logs...)
		out[i].LogEntries = append([]domain.StageLog(nil), stage.LogEntries...)
	}
	return out
}

func (p *Pipeline) set(name domain.StageName, status domain.StageStatus, log string) {
	for i := range p.stages {
		if p.stages[i].Name == name {
			p.stages[i].Status = status
			if log != "" {
				p.appendLog(i, log)
			}
		}
	}
}

func (p *Pipeline) append(name domain.StageName, log string) {
	if log == "" {
		return
	}
	for i := range p.stages {
		if p.stages[i].Name == name {
			p.appendLog(i, log)
		}
	}
}

func (p *Pipeline) appendLog(index int, text string) {
	p.stages[index].Logs = append(p.stages[index].Logs, text)
	p.stages[index].LogEntries = append(p.stages[index].LogEntries, domain.StageLog{
		CreatedAt: time.Now(),
		Text:      text,
	})
}
