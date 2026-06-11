package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const (
	typewriterDuration = 250 * time.Millisecond
	uncensorDuration   = 350 * time.Millisecond
	recensorDuration   = 400 * time.Millisecond
	animTickRate       = 50 * time.Millisecond
)

func animProgress(start time.Time, dur time.Duration) float64 {
	if start.IsZero() || dur <= 0 {
		return 1
	}
	elapsed := time.Since(start)
	if elapsed >= dur {
		return 1
	}
	return float64(elapsed) / float64(dur)
}

func typewriter(text string, progress float64) string {
	if progress >= 1 {
		return text
	}
	runes := []rune(text)
	n := int(float64(len(runes)) * progress)
	if n < 0 {
		n = 0
	}
	if n > len(runes) {
		n = len(runes)
	}
	if n == 0 {
		return "█"
	}
	result := string(runes[:n])
	if n < len(runes) {
		result += "█"
	}
	return result
}

func uncensorLine(line string, frontier int, width int) string {
	if width <= 0 || frontier >= width {
		return line
	}
	if frontier <= 0 {
		n := width
		if n > 8 {
			n = 8
		}
		return buildGradient(n)
	}
	visible := ansi.Truncate(line, frontier, "")
	remaining := width - visibleWidth(visible)
	if remaining <= 0 {
		return visible
	}
	return visible + buildGradient(remaining)
}

func recensorLine(line string, frontier int, width int) string {
	if width <= 0 || frontier >= width {
		return buildGradient(width)
	}
	if frontier <= 0 {
		return buildGradient(width)
	}
	visible := ansi.Truncate(line, frontier, "")
	remaining := width - visibleWidth(visible)
	if remaining <= 0 {
		return visible
	}
	return visible + buildGradient(remaining)
}

func uncensor(block string, progress float64, width int) string {
	if progress >= 1 || width <= 0 {
		return block
	}
	lines := strings.Split(block, "\n")
	frontier := int(progress * float64(width))
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = uncensorLine(line, frontier, width)
	}
	return strings.Join(out, "\n")
}

func recensor(block string, progress float64, width int) string {
	if progress >= 1 || width <= 0 {
		return buildGradient(width) + "\n"
	}
	lines := strings.Split(block, "\n")
	frontier := int(progress * float64(width))
	out := make([]string, len(lines))
	for i, line := range lines {
		out[i] = recensorLine(line, frontier, width)
	}
	return strings.Join(out, "\n")
}

func visibleWidth(s string) int {
	return ansi.StringWidth(s)
}

func buildGradient(cells int) string {
	if cells <= 0 {
		return ""
	}
	pattern := []rune("█▓▒░")
	var b strings.Builder
	for i := 0; i < cells; i++ {
		b.WriteRune(pattern[i%len(pattern)])
	}
	return b.String()
}

func spinner(phase float64) string {
	f := int(phase * 2) % 2
	if f == 0 {
		return "▚"
	}
	return "▞"
}

func cylonDots(phase float64) string {
	frames := [3][3]rune{
		{'●', '○', '○'},
		{'○', '●', '○'},
		{'○', '○', '●'},
	}
	n := len(frames)
	cycle := 2 * (n - 1)
	raw := int(phase*float64(cycle)) % cycle
	idx := raw
	if idx >= n {
		idx = cycle - raw
	}
	f := frames[idx]
	return string(f[:])
}
