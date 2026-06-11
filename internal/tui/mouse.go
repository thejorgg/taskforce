package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// handleMouse maps wheel scrolling and left clicks onto the same frame
// geometry View renders from, so hit-testing always matches the screen.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.quitPrompt {
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
	case m.view == viewRuns && y > f.mainY && y <= f.mainY+f.mainH:
		// content rows: rule line, column header, then one row per run
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
