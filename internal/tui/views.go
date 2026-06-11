package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/daemon"
	"github.com/thejorgg/taskforce/internal/domain"
)

// headerLines builds the header content pre-truncated to the box width so
// the header never wraps and frame() height math stays exact.
func (m Model) headerLines() []string {
	inner := maxInt(10, m.width-4)
	daemonText := "daemon stopped"
	if m.daemonOK {
		daemonText = fmt.Sprintf("daemon ok · pid %d", m.daemonState.PID)
	}
	if m.static {
		daemonText = "static result view"
	}
	welcome := "welcome back, " + m.operator
	tail := " — " + m.repo + " · " + daemonText
	if lipgloss.Width(welcome) >= inner {
		welcome = takeRunes(welcome, inner)
		tail = ""
	} else {
		tail = takeRunes(tail, inner-lipgloss.Width(welcome))
	}
	lines := []string{bright.Render(welcome) + dim.Render(tail)}
	if note := m.notification(); note != "" {
		lines = append(lines, warn.Render(takeRunes(note, inner)))
	}
	active := "no active run · type a task and press enter"
	if m.record != nil {
		active = "run " + m.record.ID + " · " + string(m.record.Status)
	} else if m.static {
		active = "run " + m.run.ID + " · finished"
	}
	lines = append(lines, dim.Render(takeRunes(fmt.Sprintf("pipeline echo → dispatch → relay → scope → exfil · %d/%d stages up · %s",
		activeCount(m.run.Stages), len(m.run.Stages), active), inner)))
	return lines
}

func (m Model) notification() string {
	if m.approvalPending() {
		return fmt.Sprintf("approval requested · %s wants: %s · ctrl+a approve / ctrl+z deny",
			m.record.Pending.Stage, takeRunes(m.record.Pending.Command, 60))
	}
	return ""
}

func (m Model) header(f frame) string {
	right := time.Now().UTC().Format("15:04:05z") + " · operator: " + m.operator
	return box("taskforce "+appVersion, right, strings.Join(f.headerLines, "\n"), m.width, toneBase)
}

func (m Model) agentRail(f frame) string {
	cards := make([]string, 0, len(stageOrder))
	for i, name := range stageOrder {
		width := f.cardW
		if i == len(stageOrder)-1 {
			width = maxInt(12, m.width-f.cardW*(len(stageOrder)-1))
		}
		stage := stageByName(m.run.Stages, name)
		cards = append(cards, m.agentCard(stage, width))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cards...)
}

func (m Model) agentCard(stage domain.StageSnapshot, width int) string {
	key := stageKey(stage.Name)
	right := strings.TrimPrefix(key, "ctrl+")
	tone := toneBase
	if viewForStage(stage.Name) == m.view {
		tone = toneAccent
	}
	if stage.Status == domain.StatusNeedsRevision || stage.Status == domain.StatusFailed {
		tone = toneErr
	}
	if stage.Name == domain.StageExfil && m.approvalPending() {
		tone = toneWarn
	}
	hint := "ambient"
	if key != "" {
		hint = strings.TrimPrefix(key, "ctrl+") + " or click"
	}
	body := strings.Join([]string{
		stageStateLine(stage),
		sparkline(stage.Status, maxInt(4, width-4)),
		dim.Render(hint),
	}, "\n")
	return box(strings.ToLower(string(stage.Name)), right, body, width, tone)
}

func (m Model) mainWindow(f frame) string {
	title, right := "taskforce · live feed", "streaming"
	tone := toneBase
	if m.view != viewFeed {
		title = string(m.view) + " · " + viewTitle(m.view)
		right = "esc to return"
		tone = toneAccent
	}
	if m.view == viewExfil && m.approvalPending() {
		tone = toneWarn
	}
	return box(title, right, m.main.View(), m.width, tone)
}

const (
	approveButton = "[ ctrl+a approve ]"
	denyButton    = "[ ctrl+z deny ]"
)

var relayPaneCommands = []string{"relay.control", "relay.build"}

func (m Model) approvalBar(f frame) string {
	pending := m.record.Pending
	buttons := okStyle.Render(approveButton) + "  " + errStyle.Render(denyButton) + "  "
	avail := maxInt(0, m.width-4-lipgloss.Width(approveButton)-lipgloss.Width(denyButton)-4)
	info := takeRunes(pending.Stage+" · "+pending.Command, avail)
	return box("release gate · "+m.record.ID, "decision required", buttons+dim.Render(info), m.width, toneWarn)
}

func (m Model) inputLine() string {
	avail := maxInt(8, m.width-8)
	prompt := accent.Render("❯ ") + dim.Render(takeRunes("type a task for the pipeline · enter to dispatch", avail))
	if m.cmdStatus != "" {
		prompt = warn.Render(takeRunes(m.cmdStatus, avail+4))
	}
	if m.cmdBuffer != "" {
		visible := m.cmdBuffer
		if runes := []rune(visible); len(runes) > avail {
			visible = string(runes[len(runes)-avail:])
		}
		prompt = accent.Render("❯ ") + bright.Render(visible) + dim.Render("█")
	}
	return box("command", "", prompt, m.width, toneAccent)
}

func (m Model) feedView() string {
	lines := []string{
		feedLine("system", "session opened · taskforce "+appVersion+" · repo "+m.repo),
	}
	if m.cfgErr != nil {
		lines = append(lines, feedLine("system", "config error: "+m.cfgErr.Error()))
	} else {
		lines = append(lines, feedLine("system", fmt.Sprintf("config loaded · %d scope hooks · control=%s build=%s",
			len(m.cfg.Scope.Hooks), stageAgentLabel(m.cfg.Relay.Control, "codex"), stageAgentLabel(m.cfg.Relay.Build, "opencode"))))
	}
	for _, stage := range m.run.Stages {
		lines = append(lines, stageLogLines(stage, strings.ToLower(string(stage.Name)))...)
	}
	if tail := m.liveEventTail(30); len(tail) > 0 {
		lines = append(lines, rule("live output"))
		lines = append(lines, tail...)
	}
	if len(lines) < 4 {
		lines = append(lines,
			feedLine("echo", "watching operator input · type a task below"),
			feedLine("dispatch", "queue idle · classifier ready"),
			feedLine("relay", "control idle · build workers available"),
			feedLine("scope", "hooks idle · waiting for relay output"),
			feedLine("exfil", "release gate idle · approval required when configured"),
		)
	}
	return strings.Join(lines, "\n")
}

// liveEventTail returns streamed output lines for the stage that is running
// right now, so the feed stays alive during long commands without
// duplicating output that already landed in stage logs.
func (m Model) liveEventTail(limit int) []string {
	if m.record == nil || !m.record.Status.Active() {
		return nil
	}
	prefix := ""
	for _, stage := range m.run.Stages {
		if stage.Status == domain.StatusRunning {
			switch stage.Name {
			case domain.StageRelay:
				prefix = "relay."
			case domain.StageScope:
				prefix = "scope."
			case domain.StageExfil:
				prefix = "exfil."
			}
		}
	}
	if prefix == "" {
		return nil
	}
	return m.eventLines(prefix, limit)
}

func (m Model) eventLines(prefix string, limit int) []string {
	lines := []string{}
	for _, event := range m.events {
		if prefix != "" && !strings.HasPrefix(event.Command, prefix) {
			continue
		}
		text := strings.TrimRight(event.Text, "\n")
		for _, line := range strings.Split(text, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			lines = append(lines, event.CreatedAt.Format("15:04:05")+" "+dim.Render(fmt.Sprintf("%-13s", event.Command))+" "+line)
		}
	}
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}

func (m Model) dispatchView() string {
	task := m.run.Task
	lines := []string{rule("task packet")}
	if task.ID == "" {
		lines = append(lines,
			"no task packet yet",
			dim.Render("type a task in the command box and press enter"),
		)
	} else {
		lines = append(lines,
			"id        "+task.ID,
			"title     "+task.Title,
			"category  "+task.Category+" · severity "+valueOr(task.Severity, "-")+" · priority "+fmt.Sprint(task.Priority),
			"source    "+task.Source+" · status "+task.Status,
		)
		if len(task.AcceptanceCriteria) > 0 {
			lines = append(lines, "", rule("acceptance criteria"))
			for _, criteria := range task.AcceptanceCriteria {
				lines = append(lines, "· "+criteria)
			}
		}
	}
	lines = append(lines, "", rule("recent activity"),
		agentTail(m.run.Stages, domain.StageDispatch, "dispatch idle · no task packet queued"))
	return strings.Join(lines, "\n")
}

func (m Model) relayView() string {
	relay := m.run.Relay
	controlState := stageWorkerState(m.run.Stages, domain.StageRelay, relay.Plan.Result != nil)
	buildState := "idle"
	if relay.BuildResult != nil {
		buildState = fmt.Sprintf("exit %d · %s", relay.BuildResult.ExitCode, relay.BuildResult.Duration.Round(time.Millisecond))
	} else if stageByName(m.run.Stages, domain.StageRelay).Status == domain.StatusRunning {
		buildState = "active"
	}
	lines := []string{
		rule("workers"),
		"worker     role      state",
		m.relayWorkerLine(0, "control", "planner", controlState, stageAgentLabel(m.cfg.Relay.Control, "codex")),
		m.relayWorkerLine(1, "build", "builder", buildState, stageAgentLabel(m.cfg.Relay.Build, "opencode")),
	}
	if relay.Attempts > 1 {
		lines = append(lines, dim.Render(fmt.Sprintf("attempts: %d", relay.Attempts)))
	}
	lines = append(lines, "", dim.Render("↑/↓ or ←/→ select relay log"))
	lines = append(lines, "", m.relayPaneView())
	return strings.Join(lines, "\n")
}

func (m Model) relayWorkerLine(index int, worker, role, state, agent string) string {
	marker := "  "
	style := dim.Render
	if m.relayPane == index {
		marker = accent.Render("▸ ")
		style = bright.Render
	}
	return fmt.Sprintf("%s%-8s %-8s %-30s %s", marker, style(worker), role, state, dim.Render("agent: "+agent))
}

func (m Model) relayPaneView() string {
	command := relayPaneCommands[m.relayPane]
	lines := []string{rule(command)}
	switch command {
	case "relay.control":
		lines = append(lines, m.relayControlPane()...)
	case "relay.build":
		lines = append(lines, m.relayBuildPane()...)
	}
	if out := m.eventLines(command, 80); len(out) > 0 {
		lines = append(lines, "", rule("process output"))
		lines = append(lines, out...)
	}
	return strings.Join(lines, "\n")
}

func (m Model) relayControlPane() []string {
	plan := m.run.Relay.Plan
	lines := []string{}
	if !plan.Created.IsZero() {
		lines = append(lines, dim.Render("created "+plan.Created.Format("15:04:05")))
	}
	if plan.Result != nil {
		lines = append(lines, commandResultLine(*plan.Result))
	}
	if summary := strings.TrimSpace(plan.Summary); summary != "" {
		lines = append(lines, "", rule("plan"))
		for _, line := range strings.Split(summary, "\n") {
			lines = append(lines, line)
		}
	} else {
		lines = append(lines, "no control output yet")
	}
	return lines
}

func (m Model) relayBuildPane() []string {
	result := m.run.Relay.BuildResult
	if result == nil {
		return []string{"no build output yet"}
	}
	lines := []string{commandResultLine(*result)}
	if out := strings.TrimSpace(result.Output()); out != "" {
		lines = append(lines, "", rule("captured output"))
		for _, line := range strings.Split(out, "\n") {
			if strings.TrimSpace(line) != "" {
				lines = append(lines, line)
			}
		}
	}
	return lines
}

func (m Model) scopeView() string {
	lines := []string{rule("review hooks")}
	if len(m.run.Review.Hooks) > 0 {
		lines = append(lines, "hook                      result    duration")
		for _, hook := range m.run.Review.Hooks {
			result := okStyle.Render("pass")
			if hook.Skipped {
				result = warn.Render("skip")
			} else if hook.ExitCode != 0 {
				result = errStyle.Render(fmt.Sprintf("exit %d", hook.ExitCode))
			}
			lines = append(lines, fmt.Sprintf("%-25s %-18s %s", hook.Name, result, hook.Duration.Round(time.Millisecond)))
		}
	} else if m.cfgErr == nil && len(m.cfg.Scope.Hooks) > 0 {
		lines = append(lines, "hook                      state")
		for _, hook := range m.cfg.Scope.Hooks {
			required := dim.Render("optional")
			if hook.Required {
				required = dim.Render("required")
			}
			lines = append(lines, fmt.Sprintf("%-25s hold      %s", "scope."+hook.Name, required))
		}
	} else {
		lines = append(lines, "no scope hooks configured", dim.Render("add hooks in taskforce.json · scope.hooks"))
	}
	if reason := strings.TrimSpace(m.run.Review.Reason); reason != "" {
		lines = append(lines, "", rule("verdict"), string(m.run.Review.Status)+" · "+reason)
	}
	if out := m.eventLines("scope.", 30); len(out) > 0 {
		lines = append(lines, "", rule("process output"))
		lines = append(lines, out...)
	}
	lines = append(lines, "", rule("recent activity"),
		agentTail(m.run.Stages, domain.StageScope, "scope idle · no hooks running"))
	return strings.Join(lines, "\n")
}

func (m Model) exfilView() string {
	release := m.run.Release
	lines := []string{rule("release policy")}
	if m.cfgErr == nil {
		policy := []string{}
		if m.cfg.Exfil.Branch != "" {
			policy = append(policy, "branch "+m.cfg.Exfil.Branch)
		}
		if m.cfg.Exfil.Commit {
			policy = append(policy, "commit")
		}
		if m.cfg.Exfil.Push {
			policy = append(policy, "push")
		}
		if m.cfg.Exfil.PR {
			policy = append(policy, "pr")
		}
		if len(policy) == 0 {
			policy = append(policy, "no release actions configured")
		}
		lines = append(lines, strings.Join(policy, " · "))
	}
	if m.approvalPending() {
		lines = append(lines, "", rule("pending decision"),
			warn.Render(m.record.Pending.Stage+" wants: "+m.record.Pending.Command),
			okStyle.Render(approveButton)+"  "+errStyle.Render(denyButton))
	}
	if len(release.Results) > 0 {
		lines = append(lines, "", rule("release actions"))
		for _, result := range release.Results {
			state := okStyle.Render("ok")
			if result.Skipped {
				state = warn.Render("skipped")
			} else if result.ExitCode != 0 {
				state = errStyle.Render(fmt.Sprintf("exit %d", result.ExitCode))
			}
			lines = append(lines, fmt.Sprintf("%-18s %-12s %s", result.Name, state, dim.Render(takeRunes(result.Command, 60))))
		}
	}
	summary := []string{}
	if release.Branch != "" {
		summary = append(summary, "branch "+release.Branch)
	}
	if release.Pushed {
		summary = append(summary, "pushed")
	}
	if release.PRURL != "" {
		summary = append(summary, "PR "+release.PRURL)
	}
	if len(summary) > 0 {
		lines = append(lines, "", rule("handoff"), strings.Join(summary, " · "))
	}
	if out := m.eventLines("exfil.", 30); len(out) > 0 {
		lines = append(lines, "", rule("process output"))
		lines = append(lines, out...)
	}
	lines = append(lines, "", rule("recent activity"),
		agentTail(m.run.Stages, domain.StageExfil, "exfil idle · no approved handoff yet"))
	return strings.Join(lines, "\n")
}

func (m Model) settingsView() string {
	active := "config unavailable: " + errText(m.cfgErr)
	if m.cfgErr == nil {
		active = fmt.Sprintf("control=%s model=%s · build=%s model=%s",
			stageAgentLabel(m.cfg.Relay.Control, "codex"), valueOr(m.cfg.Relay.Control.Model, "default"),
			stageAgentLabel(m.cfg.Relay.Build, "opencode"), valueOr(m.cfg.Relay.Build.Model, "default"))
	}
	lines := []string{
		rule("settings"),
		active,
		"profile             " + valueOr(m.cfgPaths.Profile, "unavailable"),
		"project             " + valueOr(m.cfgPaths.Project, "unavailable"),
		"workspace           " + valueOr(m.cfgPaths.Workspace, "unavailable"),
	}
	if m.cfgErr == nil && len(m.cfg.Agents) > 0 {
		names := make([]string, 0, len(m.cfg.Agents))
		for name := range m.cfg.Agents {
			names = append(names, name)
		}
		lines = append(lines, "agents              "+strings.Join(names, ", "))
	}
	lines = append(lines,
		"",
		rule("edit"),
		"set workspace relay.build.agent codex",
		"set profile relay.build.model openai/gpt-5",
		`set workspace relay.build.argv ["opencode","run","{{prompt}}"]`,
		"unset workspace relay.build.argv",
		"",
		rule("keys"),
		"ctrl+p settings     ctrl+d dispatch     ctrl+r relay",
		"ctrl+s scope        ctrl+e exfil        ctrl+o runs",
		"tab next view       esc feed            ctrl+c shutdown",
	)
	return strings.Join(lines, "\n")
}

func (m Model) runsView() string {
	lines := []string{
		rule("pipeline runs"),
		"    status             id                 age    task",
	}
	if len(m.runs) == 0 {
		lines = append(lines, "no runs yet", dim.Render("type a task in the command box and press enter"))
	}
	for i, record := range m.runs {
		marker := "  "
		if i == m.runsSel {
			marker = accent.Render("▸ ")
		}
		title := record.Run.Task.Title
		if title == "" {
			title = firstNonEmptyLine(record.Signal)
		}
		row := fmt.Sprintf("%s%s %-18s %-6s %s",
			marker,
			runStatusCell(record.Status),
			record.ID,
			shortAge(record.CreatedAt),
			takeRunes(title, maxInt(10, m.width-50)))
		if record.ID == m.activeRunID {
			row += dim.Render("  · focused")
		}
		lines = append(lines, row)
	}
	lines = append(lines, "", dim.Render("↑/↓ select · enter or click focus · esc back"))
	return strings.Join(lines, "\n")
}

func runStatusCell(status daemon.RunStatus) string {
	label := fmt.Sprintf("%-18s", status)
	switch status {
	case daemon.RunPassed:
		return okStyle.Render("✓ " + label)
	case daemon.RunFailed, daemon.RunDenied:
		return errStyle.Render("✕ " + label)
	case daemon.RunNeedsRevision:
		return errStyle.Render("◆ " + label)
	case daemon.RunAwaitingApproval:
		return warn.Render("⏸ " + label)
	case daemon.RunRunning:
		return accent.Render("● " + label)
	default:
		return dim.Render("○ " + label)
	}
}

func stageWorkerState(stages []domain.StageSnapshot, name domain.StageName, hasResult bool) string {
	stage := stageByName(stages, name)
	if stage.Status == domain.StatusRunning {
		return "active"
	}
	if hasResult {
		return "done"
	}
	return "idle"
}

func stageAgentLabel(stage config.StageConfig, fallback string) string {
	if strings.TrimSpace(stage.Run) != "" || len(stage.Argv) > 0 {
		return "custom command"
	}
	return valueOr(stage.Agent, fallback)
}

func feedLine(source, text string) string {
	return fmt.Sprintf("%s %-8s %s", time.Now().Format("15:04:05"), source, text)
}

func agentTail(stages []domain.StageSnapshot, name domain.StageName, fallback string) string {
	stage := stageByName(stages, name)
	lines := stageLogLines(stage, strings.ToLower(string(name)))
	if len(lines) == 0 {
		return fallback
	}
	return strings.Join(lines, "\n")
}

func stageLogLines(stage domain.StageSnapshot, source string) []string {
	lines := []string{}
	if len(stage.LogEntries) > 0 {
		for _, log := range stage.LogEntries {
			lines = appendLogLines(lines, log.CreatedAt, source, log.Text)
		}
		return lines
	}
	for _, log := range stage.Logs {
		lines = appendLogLines(lines, time.Time{}, source, log)
	}
	return lines
}

func appendLogLines(lines []string, createdAt time.Time, source, text string) []string {
	ts := "--:--:--"
	if !createdAt.IsZero() {
		ts = createdAt.Format("15:04:05")
	}
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, fmt.Sprintf("%s %-8s %s", ts, source, line))
		}
	}
	return lines
}

func commandResultLine(result domain.CommandResult) string {
	state := fmt.Sprintf("exit %d", result.ExitCode)
	if result.Skipped {
		state = "skipped"
	}
	start := "--:--:--"
	if !result.StartedAt.IsZero() {
		start = result.StartedAt.Format("15:04:05")
	}
	return fmt.Sprintf("%s %-8s %s", start, state, result.Duration.Round(time.Millisecond))
}

func stageByName(stages []domain.StageSnapshot, name domain.StageName) domain.StageSnapshot {
	for _, stage := range stages {
		if stage.Name == name {
			return stage
		}
	}
	return domain.StageSnapshot{Name: name, Status: domain.StatusIdle}
}

func viewForStage(name domain.StageName) viewName {
	switch name {
	case domain.StageDispatch:
		return viewDispatch
	case domain.StageRelay:
		return viewRelay
	case domain.StageScope:
		return viewScope
	case domain.StageExfil:
		return viewExfil
	default:
		return viewFeed
	}
}

func stageKey(name domain.StageName) string {
	switch name {
	case domain.StageDispatch:
		return "ctrl+d"
	case domain.StageRelay:
		return "ctrl+r"
	case domain.StageScope:
		return "ctrl+s"
	case domain.StageExfil:
		return "ctrl+e"
	default:
		return ""
	}
}

func viewTitle(view viewName) string {
	switch view {
	case viewDispatch:
		return "task packets"
	case viewRelay:
		return "implementation loop"
	case viewScope:
		return "validation gates"
	case viewExfil:
		return "release gate"
	case viewSettings:
		return "settings"
	case viewRuns:
		return "run history"
	default:
		return "live feed"
	}
}

func activeCount(stages []domain.StageSnapshot) int {
	count := 0
	for _, stage := range stages {
		if stage.Status == domain.StatusPassed || stage.Status == domain.StatusRunning {
			count++
		}
	}
	return count
}

func shortAge(at time.Time) string {
	if at.IsZero() {
		return "-"
	}
	d := time.Since(at)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func firstNonEmptyLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
