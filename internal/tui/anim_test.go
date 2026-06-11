package tui

import (
	"testing"
	"time"
)

func TestAnimProgressZeroStartReturns1(t *testing.T) {
	got := animProgress(time.Time{}, time.Second)
	if got != 1 {
		t.Fatalf("animProgress(zero) = %v, want 1", got)
	}
}

func TestAnimProgressPastDurationReturns1(t *testing.T) {
	got := animProgress(time.Now().Add(-2*time.Second), time.Second)
	if got != 1 {
		t.Fatalf("animProgress(past) = %v, want 1", got)
	}
}

func TestAnimProgressMidpoint(t *testing.T) {
	start := time.Now().Add(-150 * time.Millisecond)
	got := animProgress(start, 300*time.Millisecond)
	if got < 0.4 || got > 0.6 {
		t.Fatalf("animProgress(mid) = %v, want ~0.5", got)
	}
}

func TestTypewriterProgress0(t *testing.T) {
	got := typewriter("hello", 0)
	if got != "█" {
		t.Fatalf("typewriter(0) = %q, want █", got)
	}
}

func TestTypewriterProgress1(t *testing.T) {
	got := typewriter("hello", 1)
	if got != "hello" {
		t.Fatalf("typewriter(1) = %q, want 'hello'", got)
	}
}

func TestTypewriterProgressHalf(t *testing.T) {
	got := typewriter("hello", 0.5)
	// 5 runes * 0.5 = 2.5 → 2 revealed + █
	if got != "he█" {
		t.Fatalf("typewriter(0.5) = %q, want 'he█'", got)
	}
}

func TestSpinnerAlternates(t *testing.T) {
	s0 := spinner(0)
	s1 := spinner(0.5)
	if s0 == s1 {
		t.Fatalf("spinner should alternate: %q == %q", s0, s1)
	}
	if s0 != "▚" && s0 != "▞" {
		t.Fatalf("spinner(0) = %q, want ▚ or ▞", s0)
	}
}

func TestCylonDotsWidth3(t *testing.T) {
	for _, p := range []float64{0, 0.25, 0.5, 0.75} {
		got := cylonDots(p)
		if len([]rune(got)) != 3 {
			t.Fatalf("cylonDots(%v) = %q, want 3 runes", p, got)
		}
	}
}

func TestCylonDotsCycles(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		p := float64(i) / 20.0
		got := cylonDots(p)
		seen[got] = true
	}
	if len(seen) < 3 {
		t.Fatalf("cylonDots should cycle through 3+ states, saw %d: %v", len(seen), seen)
	}
}

func TestBuildGradientWidth(t *testing.T) {
	got := buildGradient(8)
	if len([]rune(got)) != 8 {
		t.Fatalf("buildGradient(8) = %d runes, want 8", len([]rune(got)))
	}
}

func TestBuildGradientEmpty(t *testing.T) {
	got := buildGradient(0)
	if got != "" {
		t.Fatalf("buildGradient(0) = %q, want empty", got)
	}
}

func TestUncensorLineRevealsProgressively(t *testing.T) {
	line := "hello world"
	width := 11
	early := uncensorLine(line, 3, width)
	late := uncensorLine(line, 9, width)
	// early should have more gradient chars than late
	gradientEarly := 0
	for _, r := range early {
		if r == '█' || r == '▓' || r == '▒' || r == '░' {
			gradientEarly++
		}
	}
	gradientLate := 0
	for _, r := range late {
		if r == '█' || r == '▓' || r == '▒' || r == '░' {
			gradientLate++
		}
	}
	if gradientEarly <= gradientLate {
		t.Fatalf("early gradient %d should be > late gradient %d", gradientEarly, gradientLate)
	}
}

func TestUncensorBlockFullProgress(t *testing.T) {
	block := "line 1\nline 2"
	got := uncensor(block, 1.0, 10)
	if got != block {
		t.Fatalf("uncensor(1) = %q, want original block", got)
	}
}

func TestRecensorBlockFullProgress(t *testing.T) {
	got := recensor("anything", 1.0, 10)
	if len(got) == 0 {
		t.Fatal("recensor(1) returned empty")
	}
}

func TestTypewriterEmptyString(t *testing.T) {
	got := typewriter("", 0.5)
	if got != "█" {
		t.Fatalf("typewriter(empty, 0.5) = %q, want █", got)
	}
}

func TestSpinnerConsistentWidth(t *testing.T) {
	for i := 0; i < 100; i++ {
		p := float64(i) / 100.0
		s := spinner(p)
		if len([]rune(s)) != 1 {
			t.Fatalf("spinner(%v) = %d runes, want 1", p, len([]rune(s)))
		}
	}
}
