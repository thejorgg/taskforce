package tui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func testLegendItems(n int) []legendItem {
	items := make([]legendItem, n)
	keys := []string{"ctrl+a", "ctrl+b", "ctrl+c", "ctrl+d", "ctrl+e", "ctrl+f", "ctrl+g", "ctrl+h", "ctrl+i", "ctrl+j", "ctrl+k", "ctrl+l"}
	labels := []string{"approve", "deny", "dispatch", "relay", "scope", "exfil", "runs", "settings", "next", "feed", "scroll", "shutdown"}
	for i := range items {
		items[i] = legendItem{key: keys[i%len(keys)], label: labels[i%len(labels)], action: "x"}
	}
	return items
}

func TestLegendWrapsWhenSpaceAllows(t *testing.T) {
	layout := legendFor(testLegendItems(10), 80, 6)
	if layout.grid {
		t.Fatal("legend used grid mode with enough vertical space")
	}
	if len(layout.lines) == 0 || len(layout.lines) > 6 {
		t.Fatalf("lines = %d", len(layout.lines))
	}
	for _, line := range layout.lines {
		if lipgloss.Width(line) > 80 {
			t.Fatalf("line too wide: %q", line)
		}
	}
	if len(layout.spans) != 10 {
		t.Fatalf("spans = %d, want 10", len(layout.spans))
	}
}

func TestLegendFallsBackToGridWhenSquezed(t *testing.T) {
	// 10 items at width 40 wrap to many lines; with only 2 available the
	// legend must switch to grid mode and stay within 2 lines.
	layout := legendFor(testLegendItems(10), 40, 2)
	if !layout.grid {
		t.Fatal("legend did not switch to grid mode")
	}
	if len(layout.lines) > 2 {
		t.Fatalf("grid lines = %d, want <= 2", len(layout.lines))
	}
}

func TestLegendGridNeverExceedsFourLines(t *testing.T) {
	layout := legendFor(testLegendItems(12), 60, 10)
	if layout.grid {
		// fine, but the cap below still applies
		if len(layout.lines) > 4 {
			t.Fatalf("grid lines = %d, want <= 4", len(layout.lines))
		}
	}
	squeezed := legendFor(testLegendItems(12), 30, 9)
	if squeezed.grid && len(squeezed.lines) > 4 {
		t.Fatalf("grid lines = %d, want <= 4", len(squeezed.lines))
	}
}

func TestLegendGridDistributesColumnsEqually(t *testing.T) {
	layout := legendFor(testLegendItems(8), 80, 2)
	if !layout.grid {
		t.Skip("legend fit by wrapping at this size")
	}
	perLine := (8 + len(layout.lines) - 1) / len(layout.lines)
	colWidth := 80 / perLine
	for _, span := range layout.spans {
		if span.x0%colWidth != 0 {
			t.Fatalf("span x0 = %d not aligned to column width %d", span.x0, colWidth)
		}
	}
}

func TestFrameKeepsSpyViewMinimumSixLines(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {80, 24}, {80, 16}} {
		m := baseModel(".")
		m.width, m.height = size[0], size[1]
		f := m.frame()
		if f.mainH < 3 {
			t.Fatalf("%dx%d mainH = %d", size[0], size[1], f.mainH)
		}
		if len(f.legend.lines) > 0 && f.mainH < spyMinHeight {
			t.Fatalf("%dx%d legend shown but spy view %d < %d", size[0], size[1], f.mainH, spyMinHeight)
		}
		if len(f.legend.lines) > 4 {
			t.Fatalf("%dx%d legend lines = %d, want <= 4", size[0], size[1], len(f.legend.lines))
		}
	}
}

func TestFrameGeometryMatchesRenderedHeight(t *testing.T) {
	m := baseModel(".")
	m.width, m.height = 100, 30
	m.resize()
	m.syncMain()
	f := m.frame()
	total := f.headerH + f.railH + (f.mainH + 2) + f.approvalH + inputHeight + len(f.legend.lines)
	if total > m.height {
		t.Fatalf("frame total %d exceeds terminal height %d", total, m.height)
	}
}
