// Package tui renders the TaskForce operator dashboard: live pipeline runs,
// per-stage spy views, run history, settings, and release-gate approvals.
package tui

import (
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/thejorgg/taskforce/internal/config"
	"github.com/thejorgg/taskforce/internal/daemon"
	"github.com/thejorgg/taskforce/internal/domain"
	"github.com/thejorgg/taskforce/internal/workspace"
)

type viewName string

const (
	viewFeed     viewName = "taskforce"
	viewDispatch viewName = "dispatch"
	viewRelay    viewName = "relay"
	viewScope    viewName = "scope"
	viewExfil    viewName = "exfil"
	viewRuns     viewName = "runs"
	viewSettings viewName = "settings"
)

// appVersion mirrors the CLI version string in cmd/taskforce.
const appVersion = "v0.3"

var viewCycle = []viewName{viewFeed, viewDispatch, viewRelay, viewScope, viewExfil, viewRuns, viewSettings}

type Model struct {
	repo       string
	view       viewName
	relayPane  int
	cmdBuffer  string
	cmdStatus  string
	quitPrompt bool
	main       viewport.Model
	width      int
	height     int
	operator   string
	started    time.Time

	// static is true when showing one finished run with no daemon polling.
	static bool

	run         domain.PipelineRun
	record      *daemon.RunRecord
	activeRunID string
	runs        []daemon.RunRecord // newest first
	runsSel     int
	events      []daemon.JobEvent

	daemonState daemon.State
	daemonOK    bool

	cfg      config.Config
	cfgPaths config.Paths
	cfgErr   error
}

type tickMsg time.Time

const refreshInterval = 300 * time.Millisecond

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// New shows a single finished pipeline run as a static result view.
func New(run domain.PipelineRun) Model {
	if len(run.Stages) == 0 {
		run.Stages = idleStages()
	}
	m := baseModel(run.Repo)
	m.static = true
	m.run = run
	m.syncMain()
	return m
}

// NewIdle opens the live dashboard against the repo's daemon state.
func NewIdle(repo string) Model {
	m := baseModel(repo)
	m.refresh()
	return m
}

// NewRun opens the live dashboard focused on one daemon-owned run.
func NewRun(repo, id string) Model {
	m := baseModel(repo)
	m.activeRunID = id
	m.refresh()
	return m
}

func baseModel(repo string) Model {
	return Model{
		repo:     repo,
		view:     viewFeed,
		main:     viewport.New(100, 18),
		operator: operatorName(),
		started:  time.Now(),
		run: domain.PipelineRun{
			Repo:      repo,
			StartedAt: time.Now(),
			Stages:    idleStages(),
		},
	}
}

func Show(run domain.PipelineRun) error {
	return runProgram(New(run))
}

func ShowIdle(repo string) error {
	return runProgram(NewIdle(repo))
}

func ShowRun(repo, id string) error {
	return runProgram(NewRun(repo, id))
}

func runProgram(m Model) error {
	_, err := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

func (m Model) Init() tea.Cmd {
	if m.static {
		return nil
	}
	return tickCmd()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if m.static {
			return m, nil
		}
		m.refresh()
		return m, tickCmd()
	case tea.WindowSizeMsg:
		m.width = maxInt(40, msg.Width)
		m.height = maxInt(10, msg.Height)
		m.resize()
		m.syncMain()
	case tea.MouseMsg:
		return m.handleMouse(msg)
	case tea.KeyMsg:
		key := msg.String()
		if m.quitPrompt {
			return m.updateQuitPrompt(key)
		}
		switch key {
		case "ctrl+c":
			m.quitPrompt = true
			return m, nil
		case "esc":
			m.setView(viewFeed)
		case "enter":
			if m.cmdBuffer == "" && m.view == viewRuns {
				m.focusSelectedRun()
				return m, nil
			}
			m.dispatchCommand(m.cmdBuffer)
			m.cmdBuffer = ""
		case "backspace":
			if len(m.cmdBuffer) > 0 {
				m.cmdBuffer = m.cmdBuffer[:len(m.cmdBuffer)-1]
			}
		case "tab":
			m.cycleView(1)
		case "shift+tab":
			m.cycleView(-1)
		case "ctrl+p":
			m.setView(viewSettings)
		case "ctrl+d":
			m.setView(viewDispatch)
		case "ctrl+r":
			m.setView(viewRelay)
		case "ctrl+s":
			m.setView(viewScope)
		case "ctrl+e":
			m.setView(viewExfil)
		case "ctrl+o":
			m.setView(viewRuns)
		case "ctrl+a":
			m.decide(true)
		case "ctrl+z":
			m.decide(false)
		case "left":
			if m.view == viewRelay {
				m.moveRelayPane(-1)
			}
		case "right":
			if m.view == viewRelay {
				m.moveRelayPane(1)
			}
		case "up", "ctrl+k":
			if m.view == viewRelay {
				m.moveRelayPane(-1)
			} else if m.view == viewRuns {
				m.moveRunsSelection(-1)
			} else {
				m.main.LineUp(1)
			}
		case "down", "ctrl+j":
			if m.view == viewRelay {
				m.moveRelayPane(1)
			} else if m.view == viewRuns {
				m.moveRunsSelection(1)
			} else {
				m.main.LineDown(1)
			}
		case "pgup", "ctrl+u":
			m.main.HalfViewUp()
		case "pgdown", "ctrl+n":
			m.main.HalfViewDown()
		case "home":
			m.main.GotoTop()
		case "end":
			m.main.GotoBottom()
		default:
			if len(msg.Runes) > 0 {
				text := commandTextFromRunes(msg.Runes)
				if text != "" {
					m.cmdStatus = ""
					m.cmdBuffer += text
				}
			}
		}
	}
	return m, nil
}

// refresh pulls daemon, config, and run state from disk; the daemon owns all
// pipeline execution so the TUI is a pure observer plus command submitter.
func (m *Model) refresh() {
	if m.static {
		return
	}
	state, ok, err := daemon.Status(m.repo)
	m.daemonState = state
	m.daemonOK = err == nil && ok && state.Status == "running"
	m.cfg, m.cfgPaths, m.cfgErr = config.LoadEffective(m.repo, "")
	if runs, err := daemon.ListRuns(m.repo, 50); err == nil {
		for i, j := 0, len(runs)-1; i < j; i, j = i+1, j-1 {
			runs[i], runs[j] = runs[j], runs[i]
		}
		m.runs = runs
	}
	if m.runsSel >= len(m.runs) {
		m.runsSel = maxInt(0, len(m.runs)-1)
	}
	if m.activeRunID == "" && len(m.runs) > 0 && m.runs[0].Status.Active() {
		m.activeRunID = m.runs[0].ID
	}
	if m.activeRunID != "" {
		if record, found, err := daemon.ReadRun(m.repo, m.activeRunID); err == nil && found {
			m.record = &record
			if len(record.Run.Stages) > 0 {
				m.run = record.Run
			}
			if events, err := daemon.ReadRunEvents(m.repo, m.activeRunID); err == nil {
				m.events = events
			}
		}
	}
	m.syncMain()
}

func (m *Model) dispatchCommand(raw string) {
	cmd := strings.TrimSpace(raw)
	if cmd == "" {
		return
	}
	if m.view == viewSettings && m.dispatchSettingsCommand(cmd) {
		m.syncMain()
		return
	}
	if m.handleSwitchCommand(cmd) {
		return
	}
	if m.static {
		m.cmdStatus = "static result view · run `taskforce run` to dispatch"
		return
	}
	if _, err := daemon.Start(m.repo); err != nil {
		m.cmdStatus = "daemon: " + err.Error()
		return
	}
	record, err := daemon.SubmitRun(m.repo, daemon.JobOptions{}, "tui", cmd)
	if err != nil {
		m.cmdStatus = err.Error()
		return
	}
	m.activeRunID = record.ID
	m.record = &record
	m.events = nil
	m.run = domain.PipelineRun{ID: record.ID, Repo: m.repo, StartedAt: time.Now(), Stages: idleStages()}
	m.cmdStatus = "dispatched " + record.ID
	m.setView(viewFeed)
}

func (m *Model) handleSwitchCommand(cmd string) bool {
	parts := strings.Fields(cmd)
	if len(parts) < 2 {
		return false
	}
	if parts[0] != "switch" && parts[0] != "cd" {
		return false
	}
	path := strings.Join(parts[1:], " ")
	path = strings.TrimPrefix(path, "~"+string(filepath.Separator))
	if strings.HasPrefix(parts[1], "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			path = filepath.Join(home, strings.TrimPrefix(parts[1], "~"))
		}
	}
	resolved, err := workspace.Resolve(path)
	if err != nil {
		m.cmdStatus = "switch: " + err.Error()
		return true
	}
	m.repo = resolved
	m.activeRunID = ""
	m.record = nil
	m.events = nil
	m.runs = nil
	m.run = domain.PipelineRun{Repo: m.repo, StartedAt: time.Now(), Stages: idleStages()}
	m.cmdStatus = "switched to " + m.repo
	state, _ := workspace.LoadState()
	state.ActiveRepo = m.repo
	_ = workspace.SaveState(state)
	if _, err := daemon.Start(m.repo); err != nil {
		m.cmdStatus = "switched to " + m.repo + " · daemon: " + err.Error()
	}
	m.setView(viewFeed)
	return true
}

func (m *Model) dispatchSettingsCommand(cmd string) bool {
	parts := strings.Fields(cmd)
	if len(parts) < 3 {
		return false
	}
	if parts[0] != "set" && parts[0] != "unset" {
		return false
	}
	level := config.Level(parts[1])
	paths, err := config.DiscoverPaths(m.repo, "")
	if err != nil {
		m.cmdStatus = err.Error()
		return true
	}
	target, err := config.PathForLevel(paths, level)
	if err != nil {
		m.cmdStatus = err.Error()
		return true
	}
	if parts[0] == "unset" {
		if err := config.UnsetValue(target, parts[2]); err != nil {
			m.cmdStatus = err.Error()
		} else {
			m.cmdStatus = "unset " + parts[2] + " in " + string(level)
		}
		return true
	}
	if len(parts) < 4 {
		m.cmdStatus = "usage: set profile|project|workspace path value"
		return true
	}
	value := strings.TrimSpace(strings.TrimPrefix(cmd, strings.Join(parts[:3], " ")))
	if err := config.SetValue(target, parts[2], value); err != nil {
		m.cmdStatus = err.Error()
	} else {
		m.cmdStatus = "set " + parts[2] + " in " + string(level)
	}
	return true
}

// decide answers a pending release-gate approval through the daemon.
func (m *Model) decide(approve bool) {
	if !m.approvalPending() {
		m.cmdStatus = "no approval pending"
		return
	}
	var err error
	if approve {
		err = daemon.Approve(m.repo, m.record.ID, "tui:"+m.operator)
	} else {
		err = daemon.Deny(m.repo, m.record.ID, "tui:"+m.operator)
	}
	switch {
	case err != nil:
		m.cmdStatus = err.Error()
	case approve:
		m.cmdStatus = "approved " + m.record.ID
	default:
		m.cmdStatus = "denied " + m.record.ID
	}
}

func (m Model) approvalPending() bool {
	return m.record != nil && m.record.Status == daemon.RunAwaitingApproval && m.record.Pending != nil
}

func (m *Model) moveRunsSelection(delta int) {
	if len(m.runs) == 0 {
		return
	}
	m.runsSel += delta
	if m.runsSel < 0 {
		m.runsSel = 0
	}
	if m.runsSel >= len(m.runs) {
		m.runsSel = len(m.runs) - 1
	}
	m.syncMain()
}

func (m *Model) focusSelectedRun() {
	if m.runsSel < 0 || m.runsSel >= len(m.runs) {
		return
	}
	record := m.runs[m.runsSel]
	m.activeRunID = record.ID
	m.record = &record
	if len(record.Run.Stages) > 0 {
		m.run = record.Run
	} else {
		m.run = domain.PipelineRun{ID: record.ID, Repo: m.repo, Stages: idleStages()}
	}
	if events, err := daemon.ReadRunEvents(m.repo, record.ID); err == nil {
		m.events = events
	}
	m.cmdStatus = "focused " + record.ID
	m.syncMain()
}

func (m *Model) moveRelayPane(delta int) {
	m.relayPane += delta
	if m.relayPane < 0 {
		m.relayPane = len(relayPaneCommands) - 1
	}
	if m.relayPane >= len(relayPaneCommands) {
		m.relayPane = 0
	}
	m.syncMain()
	m.main.GotoTop()
}

func (m Model) View() string {
	if m.width == 0 {
		m.width = 120
		m.height = 38
	}
	if m.quitPrompt {
		return m.quitPromptView()
	}
	f := m.frame()
	m.main.Width = maxInt(30, m.width-4)
	m.main.Height = f.mainH
	sections := []string{m.header(f)}
	if f.railH > 0 {
		sections = append(sections, m.agentRail(f))
	}
	sections = append(sections, m.mainWindow(f))
	if f.approvalH > 0 {
		sections = append(sections, m.approvalBar(f))
	}
	sections = append(sections, m.inputLine())
	if len(f.legend.lines) > 0 {
		sections = append(sections, renderLegend(f.legend, f.legendItems))
	}
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// legendItems lists the bottom key legend; action names feed mouse clicks.
func (m Model) legendItems() []legendItem {
	items := []legendItem{}
	if m.approvalPending() {
		items = append(items,
			legendItem{key: "ctrl+a", label: "approve", action: "approve"},
			legendItem{key: "ctrl+z", label: "deny", action: "deny"},
		)
	}
	items = append(items,
		legendItem{key: "enter", label: "dispatch", action: ""},
		legendItem{key: "ctrl+d", label: "dispatch", action: string(viewDispatch)},
		legendItem{key: "ctrl+r", label: "relay", action: string(viewRelay)},
		legendItem{key: "ctrl+s", label: "scope", action: string(viewScope)},
		legendItem{key: "ctrl+e", label: "exfil", action: string(viewExfil)},
		legendItem{key: "ctrl+o", label: "runs", action: string(viewRuns)},
		legendItem{key: "ctrl+p", label: "settings", action: string(viewSettings)},
		legendItem{key: "tab", label: "next view", action: "next"},
		legendItem{key: "esc", label: "feed", action: string(viewFeed)},
		legendItem{key: "↑/↓", label: "scroll", action: ""},
		legendItem{key: "ctrl+c", label: "shutdown", action: "quit"},
	)
	return items
}

func (m Model) updateQuitPrompt(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "Y", "s", "S", "ctrl+c":
		m.cmdStatus = "stopping local agents"
		return m, stopLocalAgentsAndQuit(m.repo)
	case "n", "N", "k", "K", "enter":
		m.cmdStatus = "keeping local daemon running"
		return m, tea.Quit
	case "esc":
		m.quitPrompt = false
		m.cmdStatus = "shutdown cancelled"
		return m, nil
	default:
		return m, nil
	}
}

func stopLocalAgentsAndQuit(repo string) tea.Cmd {
	return func() tea.Msg {
		_ = daemon.Stop(repo)
		return tea.Quit()
	}
}

func (m Model) quitPromptView() string {
	lines := []string{
		"taskforce shutdown",
		"",
		"Stop local agents?",
		"",
		"[y] stop local agents and quit",
		"[n] keep local daemon running and quit",
		"[enter] keep daemon",
		"[esc] cancel",
		"",
		dim.Render("ctrl-c stops local agents and quits"),
	}
	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Padding(maxInt(1, m.height/3), maxInt(2, m.width/10)).
		Foreground(cDim).
		Render(strings.Join(lines, "\n"))
}

func (m *Model) setView(view viewName) {
	for _, candidate := range viewCycle {
		if candidate == view {
			m.view = view
			m.syncMain()
			if view == viewFeed {
				m.main.GotoBottom()
			} else {
				m.main.GotoTop()
			}
			return
		}
	}
}

func (m *Model) cycleView(delta int) {
	current := 0
	for i, candidate := range viewCycle {
		if candidate == m.view {
			current = i
			break
		}
	}
	next := (current + delta + len(viewCycle)) % len(viewCycle)
	m.setView(viewCycle[next])
}

func (m *Model) resize() {
	f := m.frame()
	m.main.Width = maxInt(30, m.width-4)
	m.main.Height = f.mainH
}

// syncMain refreshes the spy viewport content while preserving the operator's
// scroll position; the feed sticks to the bottom when it was already there.
func (m *Model) syncMain() {
	atBottom := m.main.AtBottom()
	offset := m.main.YOffset
	switch m.view {
	case viewDispatch:
		m.main.SetContent(m.dispatchView())
	case viewRelay:
		m.main.SetContent(m.relayView())
	case viewScope:
		m.main.SetContent(m.scopeView())
	case viewExfil:
		m.main.SetContent(m.exfilView())
	case viewSettings:
		m.main.SetContent(m.settingsView())
	case viewRuns:
		m.main.SetContent(m.runsView())
	default:
		m.main.SetContent(m.feedView())
		if atBottom {
			m.main.GotoBottom()
		} else {
			m.main.SetYOffset(offset)
		}
		return
	}
	m.main.SetYOffset(offset)
}

func idleStages() []domain.StageSnapshot {
	return []domain.StageSnapshot{
		{Name: domain.StageEcho, Status: domain.StatusRunning, Logs: []string{"watching operator input · no active task"}},
		{Name: domain.StageDispatch, Status: domain.StatusIdle, Logs: []string{"idle · no task packet queued"}},
		{Name: domain.StageRelay, Status: domain.StatusIdle, Logs: []string{"idle · control/build waiting"}},
		{Name: domain.StageScope, Status: domain.StatusIdle, Logs: []string{"idle · no hooks running"}},
		{Name: domain.StageExfil, Status: domain.StatusSkipped, Logs: []string{"no approved handoff yet"}},
	}
}

func commandTextFromRunes(runes []rune) string {
	text := string(runes)
	if looksLikeMouseEscape(text) {
		return ""
	}
	out := strings.Builder{}
	for _, r := range runes {
		if r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if unicode.IsPrint(r) && !unicode.IsControl(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func looksLikeMouseEscape(text string) bool {
	if strings.Contains(text, "\x1b[<") {
		return true
	}
	return strings.Contains(text, "[<") && strings.ContainsAny(text, "Mm")
}

func operatorName() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		parts := strings.Split(u.Username, "/")
		return parts[len(parts)-1]
	}
	return "operator"
}
