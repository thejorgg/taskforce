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
	"github.com/thejorgg/taskforce/internal/relay/build"
	"github.com/thejorgg/taskforce/internal/relay/control"
	"github.com/thejorgg/taskforce/internal/runner"
	"github.com/thejorgg/taskforce/internal/scope"
)

type Options struct {
	Config config.Config
	Repo   string
	Runner runner.Runner
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
	run := domain.PipelineRun{ID: fmt.Sprintf("run-%d", start.Unix()), Repo: p.Options.Repo, StartedAt: start}
	p.set(domain.StageEcho, domain.StatusRunning, "normalizing signal")
	run.Signal = echo.Collector{}.FromText(source, text, nil)
	p.set(domain.StageEcho, domain.StatusPassed, "signal "+run.Signal.ID)

	p.set(domain.StageDispatch, domain.StatusRunning, "triaging signal")
	run.Task = dispatch.Dispatcher{}.Dispatch(run.Signal)
	if !run.Task.Actionable {
		p.set(domain.StageDispatch, domain.StatusFailed, "signal is not actionable")
		run.Stages = p.stages
		run.EndedAt = time.Now()
		return run
	}
	p.set(domain.StageDispatch, domain.StatusPassed, "task "+run.Task.ID)

	p.set(domain.StageRelay, domain.StatusRunning, "running Control and Build")
	controller := control.Controller{Config: p.Options.Config.Relay.Control, Runner: p.Options.Runner}
	plan := controller.Plan(ctx, run.Task)
	builder := build.Builder{Config: p.Options.Config.Relay.Build, Runner: p.Options.Runner}
	run.Relay = builder.Execute(ctx, run.Task, plan)
	if plan.Result != nil {
		p.append(domain.StageRelay, plan.Result.Output())
	}
	if run.Relay.BuildResult != nil {
		p.append(domain.StageRelay, run.Relay.BuildResult.Output())
	}
	if !run.Relay.Approved {
		p.set(domain.StageRelay, domain.StatusFailed, run.Relay.Feedback)
		run.Stages = p.stages
		run.EndedAt = time.Now()
		return run
	}
	p.set(domain.StageRelay, domain.StatusPassed, run.Relay.Feedback)

	p.set(domain.StageScope, domain.StatusRunning, "running Scope hooks")
	reviewer := scope.Reviewer{Config: p.Options.Config.Scope, Runner: p.Options.Runner}
	run.Review = reviewer.Review(ctx, run.Task, run.Relay)
	p.append(domain.StageScope, run.Review.Reason)
	for _, hook := range run.Review.Hooks {
		p.append(domain.StageScope, hook.Output())
	}
	if run.Review.Status != domain.ReviewApproved {
		p.set(domain.StageScope, domain.StatusNeedsRevision, run.Review.Reason)
		run.Stages = p.stages
		run.EndedAt = time.Now()
		return run
	}
	p.set(domain.StageScope, domain.StatusPassed, "")

	p.set(domain.StageExfil, domain.StatusRunning, "running Exfil")
	releaser := exfil.Releaser{Config: p.Options.Config.Exfil, Runner: p.Options.Runner}
	run.Release = releaser.Release(ctx, run.Task, run.Review)
	p.append(domain.StageExfil, exfil.Describe(run.Release))
	if run.Release.Skipped {
		p.set(domain.StageExfil, domain.StatusSkipped, "Exfil skipped or stopped")
	} else {
		p.set(domain.StageExfil, domain.StatusPassed, "Exfil complete")
	}
	run.Stages = p.stages
	run.EndedAt = time.Now()
	return run
}

func initialStages() []domain.StageSnapshot {
	return []domain.StageSnapshot{
		{Name: domain.StageEcho, Status: domain.StatusPending},
		{Name: domain.StageDispatch, Status: domain.StatusPending},
		{Name: domain.StageRelay, Status: domain.StatusPending},
		{Name: domain.StageScope, Status: domain.StatusPending},
		{Name: domain.StageExfil, Status: domain.StatusPending},
	}
}

func (p *Pipeline) set(name domain.StageName, status domain.StageStatus, log string) {
	for i := range p.stages {
		if p.stages[i].Name == name {
			p.stages[i].Status = status
			if log != "" {
				p.stages[i].Logs = append(p.stages[i].Logs, log)
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
			p.stages[i].Logs = append(p.stages[i].Logs, log)
		}
	}
}
