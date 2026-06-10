package tui

import (
	"fmt"
	"os/user"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/thejorgg/taskforce/internal/domain"
)

type viewName string

const (
	viewFeed     viewName = "taskforce"
	viewDispatch viewName = "dispatch"
	viewRelay    viewName = "relay"
	viewScope    viewName = "scope"
	viewExfil    viewName = "exfil"
)

type Model struct {
	run        domain.PipelineRun
	view       viewName
	cmdMode    bool
	cmdBuffer  string
	cmdStatus  string
	ctrlCArmed bool
	dead       bool
	main       viewport.Model
	width      int
	height     int
	operator   string
	started    time.Time
}

func New(run domain.PipelineRun) Model {
	if len(run.Stages) == 0 {
		run.Stages = idleStages()
	}
	m := Model{
		run:      run,
		view:     viewFeed,
		main:     viewport.New(100, 18),
		operator: operatorName(),
		started:  time.Now(),
	}
	m.syncMain()
	return m
}

func NewIdle(repo string) Model {
	now := time.Now()
	return New(domain.PipelineRun{
		ID:        "session-restored",
		Repo:      repo,
		StartedAt: now,
		Stages:    idleStages(),
	})
}

func Show(run domain.PipelineRun) error {
	_, err := tea.NewProgram(New(run), tea.WithAltScreen()).Run()
	return err
}

func ShowIdle(repo string) error {
	_, err := tea.NewProgram(NewIdle(repo), tea.WithAltScreen()).Run()
	return err
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = max(80, msg.Width)
		m.height = max(24, msg.Height)
		m.resize()
		m.syncMain()
	case tea.KeyMsg:
		key := msg.String()
		if m.dead {
			if key == "enter" {
				m.dead = false
				m.ctrlCArmed = false
				m.view = viewFeed
				m.appendFeed("system", "session reopened · operator "+m.operator+" · state restored")
			}
			return m, nil
		}
		if m.cmdMode {
			return m.updateCommand(key, msg)
		}
		switch key {
		case "ctrl+c":
			if m.ctrlCArmed {
				m.dead = true
				return m, nil
			}
			m.ctrlCArmed = true
			m.cmdStatus = "interrupt received · ctrl-c again to quit"
		case "esc":
			m.view = viewFeed
			m.ctrlCArmed = false
			m.syncMain()
		case "ctrl+x", ":":
			m.cmdMode = true
			m.cmdStatus = ""
			m.cmdBuffer = ""
		case "ctrl+d":
			m.setView(viewDispatch)
		case "ctrl+r":
			m.setView(viewRelay)
		case "ctrl+s":
			m.setView(viewScope)
		case "ctrl+e":
			m.setView(viewExfil)
		case "ctrl+a":
			if m.view == viewExfil {
				m.appendFeed("exfil", "approval recorded · push/pr handoff would run when configured")
				m.cmdStatus = "approved tf-0139"
			}
		case "ctrl+z":
			if m.view == viewExfil {
				m.appendFeed("exfil", "denied · handoff requeued to relay")
				m.cmdStatus = "denied tf-0139"
			}
		case "ctrl+j", "down", "j":
			m.main.LineDown(1)
		case "ctrl+k", "up", "k":
			m.main.LineUp(1)
		case "ctrl+u", "pgup", "u":
			m.main.HalfViewUp()
		case "ctrl+n", "pgdown", "d":
			m.main.HalfViewDown()
		}
	}
	m.main, cmd = m.main.Update(msg)
	return m, cmd
}

func (m Model) updateCommand(key string, msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key {
	case "ctrl+c":
		if m.ctrlCArmed {
			m.dead = true
			return m, nil
		}
		m.ctrlCArmed = true
		m.cmdStatus = "interrupt received · ctrl-c again to quit"
	case "esc":
		m.cmdMode = false
		m.cmdBuffer = ""
	case "enter":
		m.runCommand(m.cmdBuffer)
		m.cmdMode = false
		m.cmdBuffer = ""
	case "backspace":
		if len(m.cmdBuffer) > 0 {
			m.cmdBuffer = m.cmdBuffer[:len(m.cmdBuffer)-1]
		}
	default:
		if len(msg.Runes) > 0 {
			m.cmdBuffer += string(msg.Runes)
		}
	}
	return m, nil
}

func (m *Model) runCommand(raw string) {
	cmd := strings.TrimSpace(raw)
	if cmd == "" {
		return
	}
	m.appendFeed("cmd", cmd)
	fields := strings.Fields(strings.ToLower(cmd))
	verb := fields[0]
	arg := ""
	if len(fields) > 1 {
		arg = fields[1]
	}
	switch verb {
	case "help":
		m.appendFeed("system", "commands: help · status · check <agent> · approve <id> · deny <id> · clear · version · whoami · quit")
	case "status":
		for _, stage := range m.run.Stages {
			m.appendFeed("system", fmt.Sprintf("%-9s %s · %s", strings.ToLower(string(stage.Name)), stage.Status, firstLog(stage)))
		}
	case "check":
		switch arg {
		case "dispatch":
			m.setView(viewDispatch)
		case "relay":
			m.setView(viewRelay)
		case "scope":
			m.setView(viewScope)
		case "exfil":
			m.setView(viewExfil)
		case "echo":
			m.appendFeed("system", "echo is ambient · it has no dedicated check view")
		default:
			m.appendFeed("system", "usage: check dispatch|relay|scope|exfil")
		}
	case "dispatch", "relay", "scope", "exfil":
		m.setView(viewName(verb))
	case "approve":
		m.setView(viewExfil)
		m.cmdStatus = "approved " + valueOr(arg, "tf-0139")
		m.appendFeed("exfil", m.cmdStatus+" · handoff ready")
	case "deny":
		m.setView(viewExfil)
		m.cmdStatus = "denied " + valueOr(arg, "tf-0139")
		m.appendFeed("exfil", m.cmdStatus+" · requeued to relay")
	case "clear":
		m.main.SetContent("")
	case "version":
		m.appendFeed("system", "taskforce v0.1 · go tui · multi-agent orchestration")
	case "whoami":
		m.appendFeed("system", m.operator+" · role operator · gates: release/approval")
	case "quit", "exit":
		m.dead = true
	default:
		m.appendFeed("system", "unknown command "+verb+" · try help")
	}
	m.syncMain()
}

func (m Model) View() string {
	if m.width == 0 {
		m.width = 120
		m.height = 38
	}
	if m.dead {
		return m.deadView()
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		m.header(),
		m.agentRail(),
		m.mainWindow(),
		m.inputLine(),
		m.footer(),
	)
}

func (m Model) header() string {
	right := time.Now().UTC().Format("15:04:05z") + " · operator: " + m.operator
	notif := "0 notifications · all gates clear"
	if exfilStatus(m.run.Stages) != domain.StatusPassed {
		notif = "▲ 1 notification · exfil tf-0139 pending approval · press ctrl+e to review"
	}
	content := strings.Join([]string{
		bright.Render("welcome back, "+m.operator) + dim.Render(" — session restored · /var/lib/taskforce/state.db · uplink ok"),
		warn.Render(notif),
		dim.Render("pipeline echo → dispatch → relay → scope → exfil · " + fmt.Sprintf("%d/%d stages up", activeCount(m.run.Stages), len(m.run.Stages))),
	}, "\n")
	return box("taskforce v0.1", right, content, m.width, toneBase)
}

func (m Model) agentRail() string {
	names := []domain.StageName{domain.StageEcho, domain.StageDispatch, domain.StageRelay, domain.StageScope, domain.StageExfil}
	width := max(12, m.width/len(names))
	cards := make([]string, 0, len(names))
	for _, name := range names {
		stage := stageByName(m.run.Stages, name)
		cards = append(cards, m.agentCard(stage, width))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, cards...)
}

func (m Model) agentCard(stage domain.StageSnapshot, width int) string {
	key := stageKey(stage.Name)
	right := key
	if key != "" {
		right = strings.TrimPrefix(key, "ctrl+")
	}
	tone := toneBase
	if viewForStage(stage.Name) == m.view {
		tone = toneAccent
	}
	if stage.Status == domain.StatusNeedsRevision || stage.Status == domain.StatusFailed {
		tone = toneErr
	}
	if stage.Name == domain.StageExfil && stage.Status != domain.StatusPassed {
		tone = toneWarn
	}
	hint := "ambient"
	if key != "" {
		hint = strings.TrimPrefix(key, "ctrl+") + " to check"
	}
	body := strings.Join([]string{
		stageStateLine(stage),
		sparkline(stage.Status, max(4, width-4)),
		dim.Render(hint),
	}, "\n")
	return box(strings.ToLower(string(stage.Name)), right, body, width, tone)
}

func (m Model) mainWindow() string {
	title, right := "taskforce · live feed", "streaming"
	tone := toneBase
	if m.view != viewFeed {
		title = string(m.view) + " · " + viewTitle(m.view)
		right = "esc to return"
		tone = toneAccent
	}
	if m.view == viewExfil {
		tone = toneWarn
	}
	mainHeight := max(8, m.height-16)
	m.main.Width = max(30, m.width-4)
	m.main.Height = mainHeight
	return box(title, right, m.main.View(), m.width, tone)
}

func (m Model) inputLine() string {
	prompt := dim.Render("❯ press ctrl+x or : to type a command · try help, status, approve tf-0139")
	if m.cmdStatus != "" {
		prompt = warn.Render(m.cmdStatus)
	}
	if m.cmdMode {
		prompt = accent.Render("❯ ") + bright.Render(m.cmdBuffer) + dim.Render("█")
	}
	tone := toneBase
	if m.cmdMode {
		tone = toneAccent
	}
	return box("command", "", prompt, m.width, tone)
}

func (m Model) footer() string {
	if m.ctrlCArmed {
		return errStyle.Render("interrupt received · ctrl-c again to quit")
	}
	return dim.Render("ctrl-c twice to quit · ctrl+d dispatch · ctrl+r relay · ctrl+s scope · ctrl+e exfil · esc feed · ctrl+x command")
}

func (m Model) deadView() string {
	lines := []string{
		"^C",
		"taskforce: received interrupt · draining workers",
		"relay: build terminated · control checkpoint saved",
		"scope: hooks cancelled · cache flushed",
		"system: session closed · state persisted → /var/lib/taskforce/state.db",
		"",
		"[process exited 0]",
		"",
		"enter to reconnect█",
	}
	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Padding(max(1, m.height/3), max(2, m.width/10)).
		Foreground(cDim).
		Render(strings.Join(lines, "\n"))
}

func (m *Model) setView(view viewName) {
	if view == viewFeed || view == viewDispatch || view == viewRelay || view == viewScope || view == viewExfil {
		m.view = view
		m.ctrlCArmed = false
		m.syncMain()
	}
}

func (m *Model) resize() {
	m.main.Width = max(30, m.width-4)
	m.main.Height = max(8, m.height-16)
}

func (m *Model) syncMain() {
	switch m.view {
	case viewDispatch:
		m.main.SetContent(m.dispatchView())
	case viewRelay:
		m.main.SetContent(m.relayView())
	case viewScope:
		m.main.SetContent(m.scopeView())
	case viewExfil:
		m.main.SetContent(m.exfilView())
	default:
		m.main.SetContent(m.feedView())
		m.main.GotoBottom()
		return
	}
	m.main.GotoTop()
}

func (m Model) feedView() string {
	lines := []string{
		feedLine("system", "session opened · tty terminal · taskforce v0.1"),
		feedLine("system", "config loaded · taskforce.json · 5 stages · hooks ready"),
	}
	for _, stage := range m.run.Stages {
		for _, log := range stage.Logs {
			for _, line := range strings.Split(strings.TrimRight(log, "\n"), "\n") {
				if strings.TrimSpace(line) != "" {
					lines = append(lines, feedLine(strings.ToLower(string(stage.Name)), line))
				}
			}
		}
	}
	if len(lines) < 9 {
		lines = append(lines,
			feedLine("echo", "watching configured sources · github:issues · slack:#eng-incidents · local:stdin"),
			feedLine("dispatch", "queue idle · dedupe index warm · classifier ready"),
			feedLine("relay", "control idle · build workers available"),
			feedLine("scope", "hooks idle · waiting for relay output"),
			feedLine("exfil", "release gate idle · approval required when configured"),
		)
	}
	return strings.Join(lines, "\n")
}

func (m Model) dispatchView() string {
	taskID := valueOr(m.run.Task.ID, "tf-0139")
	category := valueOr(m.run.Task.Category, "bugfix")
	return strings.Join([]string{
		rule("queue"),
		"id        kind      scope             pri  state        age",
		fmt.Sprintf("%-9s %-9s %-17s p%-3d %-12s %s", taskID, category, "relay/client", max(1, m.run.Task.Priority/20), "forwarded", "00m38s"),
		"tf-0141   bugfix    scope/hooks       p2   queued       00m09s",
		dim.Render("depth 1 · dedupe index 412 signals · classifier configured · est 1.2k tok/packet"),
		"",
		rule("recent activity"),
		agentTail(m.run.Stages, domain.StageDispatch, "dispatch idle · no task packet queued"),
	}, "\n")
}

func (m Model) relayView() string {
	running := stageByName(m.run.Stages, domain.StageRelay).Status == domain.StatusRunning
	state := "idle"
	if running {
		state = "active"
	}
	return strings.Join([]string{
		rule("workers"),
		"worker     role      state      detail",
		fmt.Sprintf("control    planner   %-9s plan/execute loop · checkpoint 38s ago", state),
		"build-1    builder   idle       configured agent or command hook",
		"build-2    builder   idle       test/build executor · last exit unknown",
		"",
		rule("recent activity"),
		agentTail(m.run.Stages, domain.StageRelay, "relay idle · control/build waiting"),
	}, "\n")
}

func (m Model) scopeView() string {
	return strings.Join([]string{
		rule("review hooks · tf-0139"),
		"hook                      result    detail",
		"go test ./...              hold      waiting for relay output",
		"configured lint            hold      command from taskforce.json",
		"visual review              hold      operator or agent review",
		"",
		rule("approval rules"),
		"scope/clean        auto-approve when configured hooks pass",
		"release/approval   operator required before exfil push/pr",
		"",
		rule("recent activity"),
		agentTail(m.run.Stages, domain.StageScope, "scope idle · no hooks running"),
	}, "\n")
}

func (m Model) exfilView() string {
	taskID := valueOr(m.run.Task.ID, "tf-0139")
	title := valueOr(m.run.Task.Title, "pending handoff")
	branch := valueOr(m.run.Release.Branch, "taskforce/"+taskID)
	return strings.Join([]string{
		rule("handoff " + taskID),
		"title     " + title,
		"branch    " + branch + " → main",
		"diff      pending relay output",
		warn.Render("status    ◆ holding at gate release/approval — operator decision required"),
		"",
		rule("commits"),
		"pending   commit will be created by configured Exfil policy",
		"",
		accent.Render("[ ctrl+a ] approve · push + open pr") + "    " + errStyle.Render("[ ctrl+z ] deny · requeue to relay"),
		"",
		rule("recent activity"),
		agentTail(m.run.Stages, domain.StageExfil, "exfil idle · no approved handoff pending"),
	}, "\n")
}

func (m *Model) appendFeed(source, text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	if m.view == viewFeed {
		current := m.main.View()
		m.main.SetContent(current + "\n" + feedLine(source, text))
		m.main.GotoBottom()
		return
	}
	stageName := domain.StageName(strings.Title(source))
	for i := range m.run.Stages {
		if m.run.Stages[i].Name == stageName {
			m.run.Stages[i].Logs = append(m.run.Stages[i].Logs, text)
			return
		}
	}
}

func idleStages() []domain.StageSnapshot {
	return []domain.StageSnapshot{
		{Name: domain.StageEcho, Status: domain.StatusRunning, Logs: []string{"watching 0 sources · no active task"}},
		{Name: domain.StageDispatch, Status: domain.StatusPending, Logs: []string{"idle · no task packet queued"}},
		{Name: domain.StageRelay, Status: domain.StatusPending, Logs: []string{"idle · control/build waiting"}},
		{Name: domain.StageScope, Status: domain.StatusPending, Logs: []string{"idle · no hooks running"}},
		{Name: domain.StageExfil, Status: domain.StatusSkipped, Logs: []string{"no approved handoff pending"}},
	}
}

func box(title, right, content string, width int, tone lipgloss.Color) string {
	width = max(8, width)
	inner := max(1, width-4)
	border := lipgloss.NewStyle().Foreground(tone)
	top := []rune("┌" + strings.Repeat("─", width-2) + "┐")
	writeAt(top, 2, " "+title+" ")
	if right != "" && width > lipgloss.Width(right)+lipgloss.Width(title)+4 {
		writeAt(top, width-lipgloss.Width(right)-3, " "+right+" ")
	}
	out := []string{border.Render(string(top))}
	for _, raw := range strings.Split(content, "\n") {
		wrapped := lipgloss.NewStyle().Width(inner).MaxWidth(inner).Render(raw)
		for _, line := range strings.Split(wrapped, "\n") {
			out = append(out, border.Render("│")+" "+padCell(line, inner)+" "+border.Render("│"))
		}
	}
	out = append(out, border.Render("└"+strings.Repeat("─", width-2)+"┘"))
	return strings.Join(out, "\n")
}

func writeAt(line []rune, pos int, text string) {
	for i, r := range []rune(text) {
		if pos+i >= 0 && pos+i < len(line) {
			line[pos+i] = r
		}
	}
}

func padCell(line string, width int) string {
	cellWidth := lipgloss.Width(line)
	if cellWidth > width {
		return truncateCell(line, width)
	}
	return line + strings.Repeat(" ", width-cellWidth)
}

func truncateCell(line string, width int) string {
	if width <= 0 {
		return ""
	}
	var out strings.Builder
	for _, r := range line {
		next := out.String() + string(r)
		if lipgloss.Width(next) > width {
			break
		}
		out.WriteRune(r)
	}
	return padCell(out.String(), width)
}

func stageStateLine(stage domain.StageSnapshot) string {
	symbol := "○"
	switch stage.Status {
	case domain.StatusRunning:
		symbol = "●"
	case domain.StatusPassed:
		symbol = "✓"
	case domain.StatusNeedsRevision:
		symbol = "◆"
	case domain.StatusFailed:
		symbol = "✕"
	case domain.StatusSkipped:
		symbol = "○"
	}
	return statusStyle(stage.Status).Render(symbol + " " + strings.ReplaceAll(string(stage.Status), "_", " "))
}

func sparkline(status domain.StageStatus, width int) string {
	pattern := "▁▁▂▁▁▂▁▁▂▁"
	switch status {
	case domain.StatusRunning:
		pattern = "▁▂▃▄▅▆▅▄▃▂"
	case domain.StatusPassed:
		pattern = "▂▄▅▇▆▅▇▆▄▂"
	case domain.StatusNeedsRevision, domain.StatusFailed:
		pattern = "▅▂▇▁▆▂▇▁▅▂"
	case domain.StatusSkipped:
		pattern = "▂▂▂▁▁▂▂▂▁▁"
	}
	repeated := strings.Repeat(pattern, width/10+2)
	return sparkStyle(status).Render(takeRunes(repeated, width))
}

func feedLine(source, text string) string {
	return fmt.Sprintf("%s %-8s %s", time.Now().Format("15:04:05"), source, text)
}

func rule(label string) string {
	return dim.Render("── " + label + " " + strings.Repeat("─", 64))
}

func agentTail(stages []domain.StageSnapshot, name domain.StageName, fallback string) string {
	stage := stageByName(stages, name)
	if len(stage.Logs) == 0 {
		return fallback
	}
	lines := []string{}
	for _, log := range stage.Logs {
		for _, line := range strings.Split(strings.TrimRight(log, "\n"), "\n") {
			if strings.TrimSpace(line) != "" {
				lines = append(lines, feedLine(strings.ToLower(string(name)), line))
			}
		}
	}
	if len(lines) == 0 {
		return fallback
	}
	return strings.Join(lines, "\n")
}

func stageByName(stages []domain.StageSnapshot, name domain.StageName) domain.StageSnapshot {
	for _, stage := range stages {
		if stage.Name == name {
			return stage
		}
	}
	return domain.StageSnapshot{Name: name, Status: domain.StatusPending}
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
	default:
		return "live feed"
	}
}

func exfilStatus(stages []domain.StageSnapshot) domain.StageStatus {
	return stageByName(stages, domain.StageExfil).Status
}

func firstLog(stage domain.StageSnapshot) string {
	if len(stage.Logs) == 0 {
		return "no output"
	}
	return strings.TrimSpace(stage.Logs[len(stage.Logs)-1])
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

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func operatorName() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		parts := strings.Split(u.Username, "/")
		return parts[len(parts)-1]
	}
	return "operator"
}

func takeRunes(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	return string(runes[:width])
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var (
	cDim    = lipgloss.Color("60")
	cText   = lipgloss.Color("109")
	cBright = lipgloss.Color("254")
	cAcc    = lipgloss.Color("74")
	cOk     = lipgloss.Color("72")
	cWarn   = lipgloss.Color("179")
	cErr    = lipgloss.Color("167")

	toneBase   = lipgloss.Color("24")
	toneAccent = cAcc
	toneWarn   = cWarn
	toneErr    = cErr

	dim      = lipgloss.NewStyle().Foreground(cDim)
	bright   = lipgloss.NewStyle().Foreground(cBright).Bold(true)
	accent   = lipgloss.NewStyle().Foreground(cAcc)
	warn     = lipgloss.NewStyle().Foreground(cWarn)
	errStyle = lipgloss.NewStyle().Foreground(cErr)
)

func statusStyle(status domain.StageStatus) lipgloss.Style {
	switch status {
	case domain.StatusPassed:
		return lipgloss.NewStyle().Foreground(cOk)
	case domain.StatusFailed, domain.StatusNeedsRevision:
		return lipgloss.NewStyle().Foreground(cErr)
	case domain.StatusRunning:
		return lipgloss.NewStyle().Foreground(cText)
	case domain.StatusSkipped:
		return lipgloss.NewStyle().Foreground(cWarn)
	default:
		return lipgloss.NewStyle().Foreground(cDim)
	}
}

func sparkStyle(status domain.StageStatus) lipgloss.Style {
	switch status {
	case domain.StatusRunning:
		return lipgloss.NewStyle().Foreground(cAcc)
	case domain.StatusPassed:
		return lipgloss.NewStyle().Foreground(cOk)
	case domain.StatusNeedsRevision, domain.StatusFailed:
		return lipgloss.NewStyle().Foreground(cErr)
	case domain.StatusSkipped:
		return lipgloss.NewStyle().Foreground(cWarn)
	default:
		return lipgloss.NewStyle().Foreground(cDim)
	}
}
