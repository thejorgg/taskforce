package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.quitPrompt || m.fileModal != nil {
		return m, nil
	}
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		if msg.Action == tea.MouseActionPress {
			m.main.LineUp(3)
		}
		return m, nil
	case tea.MouseButtonWheelDown:
		if msg.Action == tea.MouseActionPress {
			m.main.LineDown(3)
		}
		return m, nil
	case tea.MouseButtonLeft:
		if msg.Action == tea.MouseActionPress {
			return m.handleClick(msg.X, msg.Y)
		}
	}
	return m, nil
}

func (m Model) handleClick(x, y int) (tea.Model, tea.Cmd) {
	f := m.frame()
	switch {
	case y >= f.railY && y < f.railY+f.railH:
		idx := x / maxInt(1, f.cardW)
		if idx >= len(stageOrder) {
			idx = len(stageOrder) - 1
		}
		m.setView(viewForStage(stageOrder[idx]))
	case m.view == viewSettings && y > f.mainY && y <= f.mainY+f.mainH:
		return m.handleSettingsClick(x, y, f)
	case m.view == viewRuns && y > f.mainY && y <= f.mainY+f.mainH:
		row := (y - f.mainY - 1) + m.main.YOffset
		idx := row - 2
		if idx >= 0 && idx < len(m.runs) {
			m.runsSel = idx
			m.focusSelectedRun()
		}
	case f.approvalY >= 0 && y == f.approvalY+1:
		bx := x - 2
		approveW := lipgloss.Width(approveButton)
		denyStart := approveW + 2
		if bx >= 0 && bx < approveW {
			m.decide(true)
		} else if bx >= denyStart && bx < denyStart+lipgloss.Width(denyButton) {
			m.decide(false)
		}
	case y >= f.legendY:
		line := y - f.legendY
		for _, span := range f.legend.spans {
			if span.line == line && x >= span.x0 && x < span.x1 {
				return m.applyAction(span.item.action)
			}
		}
	}
	return m, nil
}

func (m Model) handleSettingsClick(x, y int, f frame) (tea.Model, tea.Cmd) {
	contentRow := (y - f.mainY - 1) + m.main.YOffset

	if m.settingsDropdownOpen >= 0 && len(m.settingsRows) > 0 {
		row := m.settingsRows[m.settingsDropdownOpen]
		settingsContentStart := 2
		dropdownRow := m.settingsDropdownOpen
		optionStartLine := settingsContentStart + dropdownRow + 1
		optionIdx := contentRow - optionStartLine
		if optionIdx >= 0 && optionIdx < len(row.options) {
			m.applySettingsChoice(row, row.options[optionIdx])
			m.settingsDropdownOpen = -1
			m.refresh()
			m.syncMain()
			return m, nil
		}
		m.settingsDropdownOpen = -1
		m.syncMain()
		return m, nil
	}

	clickedRow := contentRow - 2
	if clickedRow >= 0 {
		dropdownOffset := 0
		for i, row := range m.settingsRows {
			mappedRow := i + dropdownOffset
			if clickedRow == mappedRow && row.editable && len(row.options) > 0 {
				m.settingsSel = i
				m.settingsDropdownOpen = i
				m.settingsDropdownCur = 0
				m.syncMain()
				return m, nil
			}
			if m.settingsDropdownOpen == i {
				dropdownOffset += len(row.options)
			}
		}
		if clickedRow < len(m.settingsRows)+dropdownOffset {
			for i := range m.settingsRows {
				if clickedRow == i+dropdownOffset || (clickedRow < i+dropdownOffset+len(m.settingsRows[i].options) && m.settingsDropdownOpen == i) {
					m.settingsSel = i
					m.syncMain()
					return m, nil
				}
			}
			sel := clickedRow
			if sel >= 0 && sel < len(m.settingsRows) {
				m.settingsSel = sel
			}
			m.syncMain()
		}
	}

	for _, hit := range m.artifactRows {
		if contentRow == hit.line {
			now := time.Now()
			if m.lastClickPath == hit.path && now.Sub(m.lastClickTime) < 400*time.Millisecond {
				abs, ok := safeArtifactPath(m.repo, hit.path)
				if ok {
					m.fileModal = &fileOpenModal{path: abs, editor: editorCmd()}
					m.lastClickPath = ""
					m.lastClickTime = time.Time{}
					m.syncMain()
					return m, nil
				}
			}
			m.lastClickX = x
			m.lastClickY = y
			m.lastClickPath = hit.path
			m.lastClickTime = now
			return m, nil
		}
	}
	return m, nil
}

func (m Model) applyAction(action string) (tea.Model, tea.Cmd) {
	switch action {
	case "":
		return m, nil
	case "quit":
		m.quitPrompt = true
	case "next":
		m.cycleView(1)
	case "approve":
		m.decide(true)
	case "deny":
		m.decide(false)
	default:
		m.setView(viewName(action))
	}
	return m, nil
}
