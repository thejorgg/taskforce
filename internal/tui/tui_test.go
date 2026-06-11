package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/thejorgg/taskforce/internal/daemon"
	"github.com/thejorgg/taskforce/internal/domain"
)

func TestTypingPreservesCommandAcrossControlScreens(t *testing.T) {
	m := newTestModel(t, 100, 30)

	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fix login")})
	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.view != viewSettings {
		t.Fatalf("view = %s, want %s", m.view, viewSettings)
	}
	if m.cmdBuffer != "fix login" {
		t.Fatalf("cmdBuffer after ctrl+p = %q", m.cmdBuffer)
	}

	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlD})
	if m.view != viewDispatch {
		t.Fatalf("view = %s, want %s", m.view, viewDispatch)
	}
	if m.cmdBuffer != "fix login" {
		t.Fatalf("cmdBuffer after ctrl+d = %q", m.cmdBuffer)
	}
}

func TestMouseEscapeFragmentsDoNotEnterCommand(t *testing.T) {
	m := newTestModel(t, 100, 30)
	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<64;20;10M")})
	if m.cmdBuffer != "" {
		t.Fatalf("cmdBuffer = %q, want empty", m.cmdBuffer)
	}

	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fix login")})
	if m.cmdBuffer != "fix login" {
		t.Fatalf("cmdBuffer = %q, want normal typing preserved", m.cmdBuffer)
	}
}

func TestTabCyclesViews(t *testing.T) {
	m := newTestModel(t, 100, 30)
	seen := []viewName{m.view}
	for range viewCycle {
		m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyTab})
		seen = append(seen, m.view)
	}
	if seen[len(seen)-1] != viewFeed {
		t.Fatalf("tab cycle did not wrap to feed: %v", seen)
	}
	if m.view != viewFeed {
		t.Fatalf("view = %s", m.view)
	}
}

func TestRunsViewSelectionAndFocus(t *testing.T) {
	m := newTestModel(t, 100, 30)
	m.runs = []daemon.RunRecord{
		{ID: "tf-b", Status: daemon.RunRunning, CreatedAt: time.Now()},
		{ID: "tf-a", Status: daemon.RunPassed, CreatedAt: time.Now().Add(-time.Minute),
			Run: domain.PipelineRun{ID: "tf-a", Stages: idleStages()}},
	}
	m.setView(viewRuns)

	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.runsSel != 1 {
		t.Fatalf("runsSel = %d, want 1", m.runsSel)
	}
	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.activeRunID != "tf-a" {
		t.Fatalf("activeRunID = %q, want tf-a", m.activeRunID)
	}
	if m.record == nil || m.record.ID != "tf-a" {
		t.Fatalf("record = %#v", m.record)
	}
}

func TestRelayViewSelectsControlAndBuildLogs(t *testing.T) {
	m := newTestModel(t, 120, 40)
	m.run.Relay.Plan.Summary = "planned"
	m.run.Relay.Plan.Created = time.Date(2026, 6, 11, 12, 0, 0, 0, time.Local)
	m.run.Relay.BuildResult = &domain.CommandResult{
		ExitCode:  0,
		Stdout:    "built",
		StartedAt: time.Date(2026, 6, 11, 12, 1, 0, 0, time.Local),
		Duration:  time.Second,
	}
	m.setView(viewRelay)

	view := m.main.View()
	if !strings.Contains(view, "relay.control") || !strings.Contains(view, "planned") {
		t.Fatalf("control pane missing: %q", view)
	}

	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	view = m.main.View()
	if !strings.Contains(view, "relay.build") || !strings.Contains(view, "built") {
		t.Fatalf("build pane missing after down: %q", view)
	}
}

func TestStageLogTimestampsComeFromSnapshot(t *testing.T) {
	m := newTestModel(t, 120, 40)
	m.run.Stages = []domain.StageSnapshot{{
		Name:   domain.StageRelay,
		Status: domain.StatusPassed,
		LogEntries: []domain.StageLog{{
			CreatedAt: time.Date(2026, 6, 11, 12, 2, 3, 0, time.Local),
			Text:      "stable timestamp",
		}},
	}}
	m.setView(viewFeed)
	view := m.main.View()
	if !strings.Contains(view, "12:02:03 relay") || !strings.Contains(view, "stable timestamp") {
		t.Fatalf("timestamped stage log missing: %q", view)
	}
}

func TestApprovalBarRendersWhenRunAwaitsDecision(t *testing.T) {
	m := newTestModel(t, 110, 36)
	m.record = &daemon.RunRecord{
		ID:     "tf-77",
		Status: daemon.RunAwaitingApproval,
		Pending: &ApprovalRequestAlias{
			RunID:   "tf-77",
			Stage:   "exfil.push",
			Command: "git push origin",
		},
	}
	if !m.approvalPending() {
		t.Fatal("approvalPending = false")
	}
	view := m.View()
	if !strings.Contains(view, "release gate · tf-77") {
		t.Fatalf("approval bar missing from view")
	}
	if !strings.Contains(view, "approve") || !strings.Contains(view, "deny") {
		t.Fatalf("approval buttons missing from view")
	}
}

// ApprovalRequestAlias keeps the test readable without importing the daemon
// struct literal twice.
type ApprovalRequestAlias = daemon.ApprovalRequest

func TestCtrlCOpensShutdownPrompt(t *testing.T) {
	m := newTestModel(t, 100, 30)

	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})

	if !m.quitPrompt {
		t.Fatal("quitPrompt = false, want true")
	}
	view := m.View()
	if !strings.Contains(view, "Stop local agents?") {
		t.Fatalf("shutdown view missing local-agent prompt: %q", view)
	}
}

func TestShutdownPromptCanStopAgentsWithCtrlC(t *testing.T) {
	m := newTestModel(t, 100, 30)
	m.quitPrompt = true

	m, cmd := updateTestModelWithCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlC})

	if cmd == nil {
		t.Fatal("ctrl+c in shutdown prompt did not return a quit command")
	}
	if m.cmdStatus != "stopping local agents" {
		t.Fatalf("cmdStatus = %q", m.cmdStatus)
	}
}

func TestShutdownPromptCanCancel(t *testing.T) {
	m := newTestModel(t, 100, 30)
	m.quitPrompt = true

	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})

	if m.quitPrompt {
		t.Fatal("quitPrompt = true, want false")
	}
	if m.cmdStatus != "shutdown cancelled" {
		t.Fatalf("cmdStatus = %q", m.cmdStatus)
	}
}

func TestMouseClickOnRailCardOpensStageView(t *testing.T) {
	m := newTestModel(t, 100, 30)
	f := m.frame()
	// third card is relay
	x := f.cardW*2 + 1
	m = updateTestModel(t, m, tea.MouseMsg{
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: f.railY + 1,
	})
	if m.view != viewRelay {
		t.Fatalf("view after rail click = %s, want %s", m.view, viewRelay)
	}
}

func TestMouseClickOnLegendSwitchesView(t *testing.T) {
	m := newTestModel(t, 120, 40)
	f := m.frame()
	var target legendSpan
	found := false
	for _, span := range f.legend.spans {
		if span.item.action == string(viewSettings) {
			target = span
			found = true
		}
	}
	if !found {
		t.Fatalf("settings legend span not found: %#v", f.legend.spans)
	}
	m = updateTestModel(t, m, tea.MouseMsg{
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
		X: target.x0, Y: f.legendY + target.line,
	})
	if m.view != viewSettings {
		t.Fatalf("view after legend click = %s, want %s", m.view, viewSettings)
	}
}

func TestStaticModelDoesNotPoll(t *testing.T) {
	m := New(domain.PipelineRun{ID: "tf-static", Repo: "."})
	if m.Init() != nil {
		t.Fatal("static model should not start the tick loop")
	}
	if !m.static {
		t.Fatal("static = false")
	}
}

func TestViewRendersAtTinySize(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}, {80, 16}, {40, 10}} {
		m := newTestModel(t, size[0], size[1])
		view := m.View()
		if strings.TrimSpace(view) == "" {
			t.Fatalf("empty view at %dx%d", size[0], size[1])
		}
	}
}

func newTestModel(t *testing.T, width, height int) Model {
	t.Helper()
	m := baseModel(t.TempDir())
	m.width = width
	m.height = height
	m.resize()
	m.syncMain()
	return m
}

func updateTestModel(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	updated, _ := updateTestModelWithCmd(t, m, msg)
	return updated
}

func updateTestModelWithCmd(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	updated, ok := next.(Model)
	if !ok {
		t.Fatalf("updated model type = %T", next)
	}
	return updated, cmd
}

func TestSwitchCommandUpdatesRepo(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t, 100, 30)
	oldRepo := m.repo

	m.dispatchCommand("switch " + repo)
	if m.repo != repo {
		t.Fatalf("repo = %q, want %q", m.repo, repo)
	}
	if m.repo == oldRepo {
		t.Fatal("repo did not change")
	}
	if !strings.Contains(m.cmdStatus, "switched to") {
		t.Fatalf("cmdStatus = %q", m.cmdStatus)
	}
	if m.activeRunID != "" {
		t.Fatalf("activeRunID = %q, want empty", m.activeRunID)
	}
	if m.record != nil {
		t.Fatal("record should be nil after switch")
	}
	if len(m.events) != 0 {
		t.Fatal("events should be empty after switch")
	}
	if len(m.runs) != 0 {
		t.Fatal("runs should be empty after switch")
	}
}

func TestCdCommandUpdatesRepo(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t, 100, 30)

	m.dispatchCommand("cd " + repo)
	if m.repo != repo {
		t.Fatalf("repo = %q, want %q", m.repo, repo)
	}
}

func TestSwitchResetsToFeedView(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	m := newTestModel(t, 100, 30)
	m.view = viewSettings

	m.dispatchCommand("switch " + repo)
	if m.view != viewFeed {
		t.Fatalf("view = %s, want %s", m.view, viewFeed)
	}
}

func TestSwitchBadPathShowsError(t *testing.T) {
	m := newTestModel(t, 100, 30)

	m.dispatchCommand("switch /no/such/path/ever")
	if m.repo == "/no/such/path/ever" {
		t.Fatal("repo should not have changed")
	}
	if !strings.Contains(m.cmdStatus, "switch:") {
		t.Fatalf("cmdStatus = %q", m.cmdStatus)
	}
}

func TestSwitchNoArgDoesNothing(t *testing.T) {
	m := newTestModel(t, 100, 30)
	oldRepo := m.repo

	m.dispatchCommand("switch")
	if m.repo != oldRepo {
		t.Fatal("repo should not have changed for bare switch")
	}
}

func TestSettingsLegendClickOpensSettings(t *testing.T) {
	m := newTestModel(t, 120, 40)
	f := m.frame()
	var target legendSpan
	found := false
	for _, span := range f.legend.spans {
		if span.item.action == string(viewSettings) {
			target = span
			found = true
		}
	}
	if !found {
		t.Fatalf("settings legend span not found")
	}
	m = updateTestModel(t, m, tea.MouseMsg{
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
		X: target.x0, Y: f.legendY + target.line,
	})
	if m.view != viewSettings {
		t.Fatalf("view = %s, want settings", m.view)
	}
}

func TestSettingsUpDownSelectsRows(t *testing.T) {
	m := newTestModel(t, 100, 30)
	m.setView(viewSettings)
	m.syncMain()
	if m.settingsSel != 0 {
		t.Fatalf("initial settingsSel = %d, want 0", m.settingsSel)
	}
	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.settingsSel != 1 {
		t.Fatalf("settingsSel after down = %d, want 1", m.settingsSel)
	}
	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyUp})
	if m.settingsSel != 0 {
		t.Fatalf("settingsSel after up = %d, want 0", m.settingsSel)
	}
}

func TestSettingsEnterOpensDropdown(t *testing.T) {
	m := newTestModel(t, 100, 30)
	m.setView(viewSettings)
	m.syncMain()
	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.settingsDropdownOpen != 0 {
		t.Fatalf("dropdownOpen = %d, want 0", m.settingsDropdownOpen)
	}
	view := m.main.View()
	if !strings.Contains(view, "codex") || !strings.Contains(view, "opencode") {
		t.Fatalf("dropdown options missing from view: %q", view)
	}
}

func TestSettingsDropdownEscCloses(t *testing.T) {
	m := newTestModel(t, 100, 30)
	m.setView(viewSettings)
	m.syncMain()
	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.settingsDropdownOpen < 0 {
		t.Fatal("dropdown did not open")
	}
	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.settingsDropdownOpen >= 0 {
		t.Fatalf("dropdownOpen = %d after esc, want -1", m.settingsDropdownOpen)
	}
}

func TestSettingsDropdownDownSelectsOption(t *testing.T) {
	m := newTestModel(t, 100, 30)
	m.setView(viewSettings)
	m.syncMain()
	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.settingsDropdownCur != 1 {
		t.Fatalf("dropdownCur = %d, want 1", m.settingsDropdownCur)
	}
}

func TestCommandBufferPreservedAcrossSettings(t *testing.T) {
	m := newTestModel(t, 100, 30)
	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("fix login")})
	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyCtrlP})
	if m.view != viewSettings {
		t.Fatalf("view = %s, want settings", m.view)
	}
	if m.cmdBuffer != "fix login" {
		t.Fatalf("cmdBuffer = %q, want 'fix login'", m.cmdBuffer)
	}
	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyDown})
	if m.cmdBuffer != "fix login" {
		t.Fatalf("cmdBuffer after nav = %q, want 'fix login'", m.cmdBuffer)
	}
}

func TestSettingsEscReturnsToFeed(t *testing.T) {
	m := newTestModel(t, 100, 30)
	m.setView(viewSettings)
	m.syncMain()
	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.view != viewFeed {
		t.Fatalf("view after esc = %s, want feed", m.view)
	}
	if m.settingsDropdownOpen != -1 {
		t.Fatalf("dropdownOpen = %d, want -1", m.settingsDropdownOpen)
	}
}

func TestDoubleClickArtifactOpensModal(t *testing.T) {
	m := newTestModel(t, 120, 40)
	m.run.Signal.Artifacts = []string{"src/main.go"}
	m.setView(viewSettings)
	m.syncMain()
	f := m.frame()
	if len(m.artifactRows) == 0 {
		t.Fatal("no artifact rows rendered")
	}
	hit := m.artifactRows[0]
	screenY := f.mainY + 1 + hit.line - m.main.YOffset
	m = updateTestModel(t, m, tea.MouseMsg{
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
		X: 4, Y: screenY,
	})
	if m.fileModal != nil {
		t.Fatal("single click should not open modal")
	}
	m = updateTestModel(t, m, tea.MouseMsg{
		Action: tea.MouseActionPress, Button: tea.MouseButtonLeft,
		X: 4, Y: screenY,
	})
	if m.fileModal == nil {
		t.Fatal("double click should open modal")
	}
}

func TestFileModalEnterLaunchesEditor(t *testing.T) {
	m := newTestModel(t, 100, 30)
	m.fileModal = &fileOpenModal{path: "/tmp/test.go", editor: "cat"}
	updated, cmd := updateTestModelWithCmd(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected editor command")
	}
	if updated.fileModal != nil {
		t.Fatal("modal should be cleared after enter")
	}
}

func TestFileModalEscCloses(t *testing.T) {
	m := newTestModel(t, 100, 30)
	m.fileModal = &fileOpenModal{path: "/tmp/test.go", editor: "cat"}
	m = updateTestModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.fileModal != nil {
		t.Fatal("modal should be nil after esc")
	}
}

func TestSafeArtifactPathRejectsTraversal(t *testing.T) {
	_, ok := safeArtifactPath("/home/user/repo", "../../../etc/passwd")
	if ok {
		t.Fatal("should reject path traversal")
	}
	_, ok = safeArtifactPath("/home/user/repo", "src/main.go")
	if !ok {
		t.Fatal("should accept relative path within repo")
	}
	abs, ok := safeArtifactPath("/home/user/repo", "/tmp/absolute.go")
	if !ok {
		t.Fatal("should accept absolute path")
	}
	if abs != "/tmp/absolute.go" {
		t.Fatalf("abs = %q, want /tmp/absolute.go", abs)
	}
}

func TestEditorCmdReturnsSomething(t *testing.T) {
	ed := editorCmd()
	t.Logf("editorCmd = %q", ed)
}
