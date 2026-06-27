// Package tui renders the TaskForce operator dashboard: live pipeline runs,
// per-stage spy views, run history, settings, and release-gate approvals.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
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

	// animation state
	viewChangedAt    time.Time
	quitPromptAt     time.Time
	quitting         bool
	quitStartedAt    time.Time
	agentsStopped    bool
	animTickInFlight bool
	animOff          bool

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

	// init wizard state
	initWizard bool
	initStep   int
	initData   initData

	// settings interactive state
	settingsSel          int
	settingsDropdownOpen int // -1 = none, index into settingsRows
	settingsDropdownCur  int // selected option within open dropdown

	// file modal state
	fileModal     *fileOpenModal
	lastClickX    int
	lastClickY    int
	lastClickPath string
	lastClickTime time.Time

	// hit-map for rendered settings rows and artifact rows
	settingsRows []settingsRow
	artifactRows []artifactHit
}

type initData struct {
	controlAgent string
	buildAgent   string
	controlModel string
	buildModel   string
	exfilBranch  string
	exfilCommit  bool
}

type fileOpenModal struct {
	path   string
	editor string
}

type settingsRow struct {
	label    string
	dotted   string
	value    string
	options  []string
	editable bool
}

type artifactHit struct {
	path string
	line int
}

type tickMsg time.Time
type animMsg time.Time
type agentsStoppedMsg struct{}

const refreshInterval = 300 * time.Millisecond

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func animTickCmd() tea.Cmd {
	return tea.Tick(animTickRate, func(t time.Time) tea.Msg { return animMsg(t) })
}

func maybeAnimTick(m Model) tea.Cmd {
	if m.animActive() && !m.animTickInFlight {
		return animTickCmd()
	}
	return nil
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
		repo:                 repo,
		view:                 viewFeed,
		main:                 viewport.New(100, 18),
		operator:             operatorName(),
		started:              time.Now(),
		settingsDropdownOpen: -1,
		animOff:              os.Getenv("TASKFORCE_REDUCE_MOTION") != "",
		viewChangedAt:        time.Now(),
		run: domain.PipelineRun{
			Repo:      repo,
			StartedAt: time.Now(),
			Stages:    idleStages(),
		},
	}
}

func (m Model) animActive() bool {
	if m.animOff {
		return false
	}
	if !m.viewChangedAt.IsZero() && animProgress(m.viewChangedAt, uncensorDuration) < 1 {
		return true
	}
	if m.quitPrompt && !m.quitPromptAt.IsZero() && animProgress(m.quitPromptAt, uncensorDuration) < 1 {
		return true
	}
	if m.quitting && !m.quitStartedAt.IsZero() && animProgress(m.quitStartedAt, recensorDuration) < 1 {
		return true
	}
	return false
}

func (m Model) phase() float64 {
	if m.started.IsZero() {
		return 0
	}
	return float64(time.Since(m.started)) / float64(refreshInterval)
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
	return tea.Batch(tickCmd(), maybeAnimTick(m))
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		if m.static {
			return m, nil
		}
		if m.quitting && (m.agentsStopped || time.Since(m.quitStartedAt) >= recensorDuration) {
			return m, tea.Quit
		}
		m.refresh()
		return m, tea.Batch(tickCmd(), maybeAnimTick(m))
	case animMsg:
		m.animTickInFlight = false
		if m.quitting && (m.agentsStopped || time.Since(m.quitStartedAt) >= recensorDuration) {
			return m, tea.Quit
		}
		if !m.animActive() {
			return m, nil
		}
		return m, maybeAnimTick(m)
	case agentsStoppedMsg:
		m.agentsStopped = true
		return m, tea.Quit
	case editorResult:
		if m.fileModal != nil {
			if msg.err != nil {
				m.cmdStatus = "editor: " + msg.err.Error()
			} else {
				m.cmdStatus = "opened " + m.fileModal.path
			}
			m.fileModal = nil
			m.syncMain()
		}
		return m, nil
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
		if m.initWizard {
			return m.updateInitWizard(key)
		}
		if m.fileModal != nil {
			return m, m.updateFileModal(key)
		}
		if m.view == viewSettings && m.settingsDropdownOpen >= 0 {
			m.updateSettingsDropdown(key)
			return m, nil
		}
		if m.view == viewSettings {
			if m.updateSettingsNav(key) {
				return m, nil
			}
		}
		switch key {
		case "ctrl+c":
			m.quitPrompt = true
			m.quitPromptAt = time.Now()
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
	state, ok, err := daemon.Status()
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
	if m.handleSlashCommand(cmd) {
		return
	}
	if m.handleSwitchCommand(cmd) {
		return
	}
	if m.view == viewSettings {
		if m.dispatchSettingsCommand(cmd) {
			m.syncMain()
			return
		}
		m.cmdStatus = "command blocked in settings · set/unset to edit · esc to return"
		m.syncMain()
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

func (m *Model) handleSlashCommand(cmd string) bool {
	if !strings.HasPrefix(cmd, "/") {
		return false
	}
	switch cmd {
	case "/settings":
		m.setView(viewSettings)
		return true
	case "/history":
		m.setView(viewRuns)
		return true
	case "/init":
		m.initWizard = true
		m.initStep = 0
		m.initData = initData{}
		m.cmdStatus = ""
		m.syncMain()
		return true
	default:
		m.cmdStatus = "unknown command: " + cmd
		return true
	}
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

func (m *Model) updateInitWizard(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.initWizard = false
		m.initStep = 0
		m.cmdStatus = "init cancelled"
		m.syncMain()
		return m, nil
	case "enter":
		cmd := strings.TrimSpace(m.cmdBuffer)
		m.cmdBuffer = ""
		m.cmdStatus = ""
		switch m.initStep {
		case 0:
			m.initData.controlAgent = resolveAgentChoice(cmd, "codex")
			m.initStep = 1
		case 1:
			m.initData.controlModel = cmd
			m.initStep = 2
		case 2:
			m.initData.buildAgent = resolveAgentChoice(cmd, "opencode")
			m.initStep = 3
		case 3:
			m.initData.buildModel = cmd
			m.initStep = 4
		case 4:
			if cmd == "" {
				m.initData.exfilBranch = "taskforce/{{task_id}}"
			} else {
				m.initData.exfilBranch = cmd
			}
			m.initData.exfilCommit = true
			m.completeInitWizard()
			return m, nil
		}
		m.syncMain()
		return m, nil
	case "backspace":
		if len(m.cmdBuffer) > 0 {
			m.cmdBuffer = m.cmdBuffer[:len(m.cmdBuffer)-1]
		}
	default:
		runes := []rune(key)
		if len(runes) == 1 && runes[0] >= 32 {
			m.cmdBuffer += key
		}
	}
	return m, nil
}

func resolveAgentChoice(input, fallback string) string {
	input = strings.TrimSpace(input)
	if input == "" {
		return fallback
	}
	switch input {
	case "1", "codex":
		return "codex"
	case "2", "opencode":
		return "opencode"
	case "3", "claude":
		return "claude"
	case "4", "mimo":
		return "mimo"
	default:
		return input
	}
}

func (m *Model) completeInitWizard() {
	data := m.initData
	cfg := config.Default()
	cfg.Relay.Control.Agent = data.controlAgent
	cfg.Relay.Control.Model = data.controlModel
	cfg.Relay.Build.Agent = data.buildAgent
	cfg.Relay.Build.Model = data.buildModel
	cfg.Exfil.Branch = data.exfilBranch
	cfg.Exfil.Commit = data.exfilCommit

	paths, err := config.DiscoverPaths(m.repo, "")
	if err != nil {
		m.cmdStatus = "init: " + err.Error()
		m.initWizard = false
		m.syncMain()
		return
	}
	if err := config.WriteDefault(paths.Project); err != nil {
		m.cmdStatus = "init: " + err.Error()
		m.initWizard = false
		m.syncMain()
		return
	}

	agentsMD := `# Agents

## Control Agent
- Agent: ` + data.controlAgent + `
- Model: ` + valueOr(data.controlModel, "default") + `
- Role: Plans implementation approach

## Build Agent
- Agent: ` + data.buildAgent + `
- Model: ` + valueOr(data.buildModel, "default") + `
- Role: Implements approved plans
`
	agentsPath := filepath.Join(m.repo, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte(agentsMD), 0o644); err != nil {
		m.cmdStatus = "init: wrote config but failed to write AGENTS.md: " + err.Error()
	} else {
		m.cmdStatus = "init complete · taskforce.json + AGENTS.md created"
	}
	m.initWizard = false
	m.initStep = 0
	m.refresh()
	m.syncMain()
}

func (m *Model) updateSettingsNav(key string) bool {
	rows := m.buildSettingsRows()
	switch key {
	case "up", "ctrl+k":
		if m.settingsSel > 0 {
			m.settingsSel--
		}
		m.syncMain()
		return true
	case "down", "ctrl+j":
		if m.settingsSel < len(rows)-1 {
			m.settingsSel++
		}
		m.syncMain()
		return true
	case "enter":
		if m.settingsSel >= 0 && m.settingsSel < len(rows) {
			row := rows[m.settingsSel]
			if row.editable && len(row.options) > 0 {
				m.settingsDropdownOpen = m.settingsSel
				m.settingsDropdownCur = 0
				m.syncMain()
				return true
			}
		}
	}
	return false
}

func (m *Model) updateSettingsDropdown(key string) {
	rows := m.buildSettingsRows()
	if m.settingsDropdownOpen < 0 || m.settingsDropdownOpen >= len(rows) {
		m.settingsDropdownOpen = -1
		m.syncMain()
		return
	}
	row := rows[m.settingsDropdownOpen]
	switch key {
	case "esc":
		m.settingsDropdownOpen = -1
		m.syncMain()
		return
	case "up", "ctrl+k":
		if m.settingsDropdownCur > 0 {
			m.settingsDropdownCur--
		}
		m.syncMain()
		return
	case "down", "ctrl+j":
		if m.settingsDropdownCur < len(row.options)-1 {
			m.settingsDropdownCur++
		}
		m.syncMain()
		return
	case "enter":
		choice := row.options[m.settingsDropdownCur]
		m.applySettingsChoice(row, choice)
		m.settingsDropdownOpen = -1
		m.refresh()
		m.syncMain()
		return
	}
}

func (m *Model) applySettingsChoice(row settingsRow, choice string) {
	paths, err := config.DiscoverPaths(m.repo, "")
	if err != nil {
		m.cmdStatus = err.Error()
		return
	}
	level := config.LevelWorkspace
	if row.dotted == "config.level" {
		return
	}
	target, err := config.PathForLevel(paths, level)
	if err != nil {
		m.cmdStatus = err.Error()
		return
	}
	if choice == "true" || choice == "false" {
		if err := config.SetValue(target, row.dotted, choice); err != nil {
			m.cmdStatus = err.Error()
		} else {
			m.cmdStatus = "set " + row.dotted + " = " + choice
		}
	} else {
		if err := config.SetValue(target, row.dotted, choice); err != nil {
			m.cmdStatus = err.Error()
		} else {
			m.cmdStatus = "set " + row.dotted + " = " + choice
		}
	}
}

func (m *Model) updateFileModal(key string) tea.Cmd {
	switch key {
	case "enter":
		editor := m.fileModal.editor
		path := m.fileModal.path
		m.fileModal = nil
		m.syncMain()
		if editor == "" {
			m.cmdStatus = "no $EDITOR set · set VISUAL or EDITOR environment variable"
			return nil
		}
		m.cmdStatus = "opening " + path + " with " + editor
		return openEditorCmd(editor, path)
	case "esc":
		m.fileModal = nil
		m.syncMain()
		return nil
	}
	return nil
}

func (m *Model) buildSettingsRows() []settingsRow {
	rows := []settingsRow{}
	if m.cfgErr != nil {
		return rows
	}
	cfg := m.cfg
	rows = append(rows,
		settingsRow{label: "control agent", dotted: "relay.control.agent", value: valueOr(cfg.Relay.Control.Agent, "codex"), options: []string{"codex", "opencode", "claude", "mimo"}, editable: true},
		settingsRow{label: "control model", dotted: "relay.control.model", value: valueOr(cfg.Relay.Control.Model, "default"), options: nil, editable: false},
		settingsRow{label: "build agent", dotted: "relay.build.agent", value: valueOr(cfg.Relay.Build.Agent, "opencode"), options: []string{"codex", "opencode", "claude", "mimo"}, editable: true},
		settingsRow{label: "build model", dotted: "relay.build.model", value: valueOr(cfg.Relay.Build.Model, "default"), options: nil, editable: false},
		settingsRow{label: "exfil.commit", dotted: "exfil.commit", value: fmt.Sprintf("%v", cfg.Exfil.Commit), options: []string{"true", "false"}, editable: true},
		settingsRow{label: "exfil.push", dotted: "exfil.push", value: fmt.Sprintf("%v", cfg.Exfil.Push), options: []string{"true", "false"}, editable: true},
		settingsRow{label: "exfil.pr", dotted: "exfil.pr", value: fmt.Sprintf("%v", cfg.Exfil.PR), options: []string{"true", "false"}, editable: true},
		settingsRow{label: "rescue.enabled", dotted: "rescue.enabled", value: fmt.Sprintf("%v", cfg.Rescue.Enabled), options: []string{"true", "false"}, editable: true},
		settingsRow{label: "profile path", dotted: "", value: valueOr(m.cfgPaths.Profile, "unavailable"), options: nil, editable: false},
		settingsRow{label: "project path", dotted: "", value: valueOr(m.cfgPaths.Project, "unavailable"), options: nil, editable: false},
		settingsRow{label: "workspace path", dotted: "", value: valueOr(m.cfgPaths.Workspace, "unavailable"), options: nil, editable: false},
	)
	return rows
}

func (m *Model) artifacts() []string {
	seen := map[string]bool{}
	var arts []string
	for _, a := range m.run.Signal.Artifacts {
		if !seen[a] {
			seen[a] = true
			arts = append(arts, a)
		}
	}
	for _, a := range m.run.Task.RelevantArtifacts {
		if !seen[a] {
			seen[a] = true
			arts = append(arts, a)
		}
	}
	return arts
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
	if m.fileModal != nil {
		return m.fileModalView()
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

func (m Model) fileModalView() string {
	editor := m.fileModal.editor
	if editor == "" {
		editor = "(none set)"
	}
	editorLine := "[enter] open with " + editor
	if m.fileModal.editor == "" {
		editorLine = "[enter] no $EDITOR set · set VISUAL or EDITOR"
	}
	lines := []string{
		"",
		bright.Render("  open file"),
		"",
		"  " + m.fileModal.path,
		"",
		"  editor: " + dim.Render(editor),
		"",
		"  " + editorLine,
		"  " + dim.Render("[esc] cancel"),
		"",
	}
	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Padding(maxInt(1, m.height/3), maxInt(2, m.width/10)).
		Foreground(cDim).
		Render(strings.Join(lines, "\n"))
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
		m.quitting = true
		m.quitStartedAt = time.Now()
		m.animTickInFlight = false
		return m, tea.Batch(stopLocalAgentsCmd(m.repo), maybeAnimTick(m))
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

func stopLocalAgentsCmd(repo string) tea.Cmd {
	return func() tea.Msg {
		_ = daemon.Stop(repo)
		return agentsStoppedMsg{}
	}
}

func (m Model) quitPromptView() string {
	title := "taskforce shutdown"
	options := "Stop local agents?\n\n[y] stop local agents and quit\n[n] keep local daemon running and quit\n[enter] keep daemon\n[esc] cancel\n\n" + dim.Render("ctrl-c stops local agents and quits")
	if !m.animOff && !m.quitPromptAt.IsZero() {
		p := animProgress(m.quitPromptAt, typewriterDuration)
		title = typewriter(title, p)
		up := animProgress(m.quitPromptAt, uncensorDuration)
		options = uncensor(options, up, maxInt(10, m.width-4))
	}
	if m.quitting {
		sp := spinner(animProgress(m.quitStartedAt, 200*time.Millisecond))
		title = "STOPPING AGENTS " + sp
		if !m.animOff {
			rp := animProgress(m.quitStartedAt, recensorDuration)
			options = recensor("Stop local agents?\n\n[y] stop local agents and quit\n[n] keep local daemon running and quit", rp, maxInt(10, m.width-4))
		}
	}
	lines := []string{
		title,
		"",
		options,
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
			if m.view == viewSettings && view != viewSettings {
				m.settingsSel = 0
				m.settingsDropdownOpen = -1
				m.settingsDropdownCur = 0
			}
			if m.view != view {
				m.viewChangedAt = time.Now()
			}
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
	if m.initWizard {
		m.main.SetContent(m.initWizardView())
		m.main.SetYOffset(offset)
		return
	}
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
		m.buildSettingsHitMap()
		m.main.SetContent(m.settingsViewContent())
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

func (m *Model) buildSettingsHitMap() {
	m.settingsRows = m.buildSettingsRows()
	m.artifactRows = nil
	arts := m.artifacts()
	if len(arts) == 0 {
		return
	}
	content := m.settingsViewContent()
	rowLines := strings.Split(content, "\n")
	for i, line := range rowLines {
		trimmed := strings.TrimSpace(line)
		for _, a := range arts {
			if trimmed == a || strings.HasSuffix(trimmed, a) {
				m.artifactRows = append(m.artifactRows, artifactHit{path: a, line: i})
			}
		}
	}
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

func editorCmd() string {
	if v := os.Getenv("VISUAL"); v != "" {
		return v
	}
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	switch runtime.GOOS {
	case "darwin":
		return "open"
	case "windows":
		return "notepad"
	default:
		if p, err := exec.LookPath("xdg-open"); err == nil {
			return p
		}
		return ""
	}
}

func openEditorCmd(editor, filePath string) tea.Cmd {
	return func() tea.Msg {
		parts := strings.Fields(editor)
		if len(parts) == 0 {
			return editorResult{err: fmt.Errorf("no $EDITOR set")}
		}
		parts = append(parts, filePath)
		cmd := exec.Command(parts[0], parts[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		return editorResult{err: err}
	}
}

type editorResult struct {
	err error
}

func safeArtifactPath(repo, raw string) (string, bool) {
	if raw == "" {
		return "", false
	}
	if filepath.IsAbs(raw) {
		return raw, true
	}
	abs := filepath.Join(repo, raw)
	abs = filepath.Clean(abs)
	root := filepath.Clean(repo)
	if !strings.HasPrefix(abs, root+string(filepath.Separator)) && abs != root {
		return "", false
	}
	return abs, true
}
