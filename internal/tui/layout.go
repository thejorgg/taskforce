package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/thejorgg/taskforce/internal/domain"
)

// legendItem is one key binding shown in the bottom legend. Action is the
// model action triggered when the item is clicked ("" = not clickable).
type legendItem struct {
	key    string
	label  string
	action string
}

func (it legendItem) text() string {
	return it.key + " " + it.label
}

// legendSpan records where one legend item was rendered for hit-testing.
type legendSpan struct {
	item legendItem
	line int
	x0   int
	x1   int // exclusive
}

type legendLayout struct {
	lines []string
	spans []legendSpan
	grid  bool
}

const legendSep = " · "

// legendFor lays out the legend for the available height. The legend prefers
// natural wrapping; the spy view above shrinks to make room until it reaches
// its 6-line minimum. Past that point the legend switches to a left-to-right
// grid of at most 4 lines with equally distributed columns.
func legendFor(items []legendItem, width, available int) legendLayout {
	if len(items) == 0 || width < 4 || available < 1 {
		return legendLayout{}
	}
	wrapped := wrapLegend(items, width)
	if len(wrapped.lines) <= available {
		return wrapped
	}
	lines := available
	if lines > 4 {
		lines = 4
	}
	return gridLegend(items, width, lines)
}

// wrapLegend joins items with separators and wraps at item boundaries.
func wrapLegend(items []legendItem, width int) legendLayout {
	out := legendLayout{}
	line := ""
	lineIdx := 0
	for _, item := range items {
		text := item.text()
		candidate := text
		if line != "" {
			candidate = line + legendSep + text
		}
		if line != "" && lipgloss.Width(candidate) > width {
			out.lines = append(out.lines, line)
			lineIdx++
			line = ""
			candidate = text
		}
		x0 := 0
		if line != "" {
			x0 = lipgloss.Width(line) + lipgloss.Width(legendSep)
		}
		out.spans = append(out.spans, legendSpan{item: item, line: lineIdx, x0: x0, x1: x0 + lipgloss.Width(text)})
		line = candidate
	}
	if line != "" {
		out.lines = append(out.lines, line)
	}
	return out
}

// gridLegend distributes items left to right across at most maxLines lines
// with equal column widths.
func gridLegend(items []legendItem, width, maxLines int) legendLayout {
	out := legendLayout{grid: true}
	if maxLines < 1 {
		return out
	}
	perLine := (len(items) + maxLines - 1) / maxLines
	if perLine < 1 {
		perLine = 1
	}
	colWidth := width / perLine
	if colWidth < 4 {
		colWidth = 4
	}
	lines := []string{}
	for start := 0; start < len(items); start += perLine {
		end := start + perLine
		if end > len(items) {
			end = len(items)
		}
		row := items[start:end]
		line := ""
		lineIdx := len(lines)
		for col, item := range row {
			cell := truncateCell(item.text(), colWidth-1)
			x0 := col * colWidth
			out.spans = append(out.spans, legendSpan{item: item, line: lineIdx, x0: x0, x1: x0 + lipgloss.Width(strings.TrimRight(cell, " "))})
			line += cell + " "
		}
		lines = append(lines, strings.TrimRight(line, " "))
		if len(lines) == maxLines {
			break
		}
	}
	out.lines = lines
	return out
}

// renderLegend styles the computed legend lines: keys accented, labels dim.
func renderLegend(layout legendLayout, items []legendItem) string {
	styled := make([]string, len(layout.lines))
	for i, line := range layout.lines {
		styled[i] = line
	}
	for i := range styled {
		out := styled[i]
		for _, item := range items {
			out = strings.Replace(out, item.key+" "+item.label, accent.Render(item.key)+" "+dim.Render(item.label), 1)
			out = strings.Replace(out, item.key+" ", accent.Render(item.key)+" ", 1)
		}
		styled[i] = out
	}
	return strings.Join(styled, "\n")
}

// frame describes the vertical geometry of the dashboard for one terminal
// size; both View and the mouse hit-testing derive from the same frame.
type frame struct {
	width, height int
	headerLines   []string
	headerH       int
	railY, railH  int
	cardW         int
	mainY, mainH  int // mainH is the spy viewport content height
	approvalY     int // -1 when no approval bar
	approvalH     int
	inputY        int
	legendY       int
	legend        legendLayout
	legendItems   []legendItem
}

const (
	railHeight   = 5 // card boxes: border + 3 content lines + border
	inputHeight  = 3
	spyMinHeight = 6
)

func (m Model) frame() frame {
	f := frame{width: m.width, height: m.height}
	f.headerLines = m.headerLines()
	// box() wraps long content lines; measure the rendered height so the
	// frame math matches what actually lands on screen.
	inner := maxInt(1, f.width-4)
	wrapStyle := lipgloss.NewStyle().Width(inner).MaxWidth(inner)
	headerRows := 0
	for _, line := range f.headerLines {
		headerRows += lipgloss.Height(wrapStyle.Render(line))
	}
	f.headerH = headerRows + 2
	f.railY = f.headerH
	f.railH = railHeight
	f.cardW = maxInt(12, f.width/len(stageOrder))
	f.approvalH = 0
	f.approvalY = -1
	if m.approvalPending() {
		f.approvalH = 3
	}
	f.legendItems = m.legendItems()
	chrome := func() int { return f.headerH + f.railH + 2 + f.approvalH + inputHeight }
	available := f.height - chrome()
	// Tight terminals shed chrome before the spy view loses its minimum:
	// first the header detail lines, then the whole stage rail.
	if available < spyMinHeight && len(f.headerLines) > 1 {
		f.headerLines = f.headerLines[:1]
		f.headerH = 3
		available = f.height - chrome()
	}
	if available < spyMinHeight {
		f.railH = 0
		available = f.height - chrome()
	}
	legendAvail := available - spyMinHeight
	if legendAvail > 0 {
		f.legend = legendFor(f.legendItems, f.width, legendAvail)
	}
	f.mainH = available - len(f.legend.lines)
	if f.mainH < spyMinHeight {
		f.mainH = maxInt(1, available)
	}
	f.mainY = f.railY + f.railH
	next := f.mainY + f.mainH + 2
	if f.approvalH > 0 {
		f.approvalY = next
		next += f.approvalH
	}
	f.inputY = next
	f.legendY = f.inputY + inputHeight
	return f
}

var stageOrder = []domain.StageName{domain.StageEcho, domain.StageDispatch, domain.StageRelay, domain.StageScope, domain.StageExfil}

func box(title, right, content string, width int, tone lipgloss.Color) string {
	width = maxInt(8, width)
	inner := maxInt(1, width-4)
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

func rule(label string) string {
	return dim.Render("── " + label + " " + strings.Repeat("─", 64))
}

func takeRunes(value string, width int) string {
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	return string(runes[:width])
}

func maxInt(a, b int) int {
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
	okStyle  = lipgloss.NewStyle().Foreground(cOk)
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
