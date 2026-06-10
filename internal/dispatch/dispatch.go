package dispatch

import (
	"crypto/sha1"
	"fmt"
	"strings"
	"time"

	"github.com/thejorgg/taskforce/internal/domain"
)

type Dispatcher struct {
	Now func() time.Time
}

func (d Dispatcher) Dispatch(signal domain.Signal) domain.TaskPacket {
	now := time.Now
	if d.Now != nil {
		now = d.Now
	}
	content := strings.TrimSpace(signal.Content)
	actionable := content != ""
	category := classify(content)
	severity, priority := severity(content)
	title := firstLine(content)
	if title == "" {
		title = "Empty signal"
	}
	if len(title) > 80 {
		title = title[:77] + "..."
	}
	status := "ready"
	if !actionable {
		status = "not_actionable"
	}
	return domain.TaskPacket{
		ID:                 taskID(signal),
		Title:              title,
		Description:        content,
		Source:             signal.Source,
		Severity:           severity,
		Priority:           priority,
		Category:           category,
		RelevantArtifacts:  signal.Artifacts,
		AcceptanceCriteria: criteriaFor(category),
		Status:             status,
		Actionable:         actionable,
		Signal:             signal,
		CreatedAt:          now(),
		Metadata:           domain.StringTable{"dedupe_key": dedupeKey(signal)},
	}
}

func taskID(signal domain.Signal) string {
	return "task-" + dedupeKey(signal)[:10]
}

func dedupeKey(signal domain.Signal) string {
	sum := sha1.Sum([]byte(strings.ToLower(strings.TrimSpace(signal.Content))))
	return fmt.Sprintf("%x", sum[:])
}

func firstLine(content string) string {
	lines := strings.Split(content, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

func classify(content string) string {
	lower := strings.ToLower(content)
	switch {
	case strings.Contains(lower, "crash"), strings.Contains(lower, "panic"), strings.Contains(lower, "exception"):
		return "bug"
	case strings.Contains(lower, "feature"), strings.Contains(lower, "request"), strings.Contains(lower, "add "):
		return "feature"
	case strings.Contains(lower, "slow"), strings.Contains(lower, "performance"), strings.Contains(lower, "latency"):
		return "performance"
	case strings.Contains(lower, "security"), strings.Contains(lower, "vulnerability"), strings.Contains(lower, "xss"):
		return "security"
	default:
		return "task"
	}
}

func severity(content string) (string, int) {
	lower := strings.ToLower(content)
	switch {
	case strings.Contains(lower, "critical"), strings.Contains(lower, "production down"), strings.Contains(lower, "data loss"):
		return "critical", 100
	case strings.Contains(lower, "crash"), strings.Contains(lower, "security"), strings.Contains(lower, "broken"):
		return "high", 80
	case strings.Contains(lower, "minor"), strings.Contains(lower, "polish"):
		return "low", 30
	default:
		return "medium", 50
	}
}

func criteriaFor(category string) []string {
	return []string{
		"Implementation addresses the task packet description.",
		"Relevant configured Scope hooks pass.",
		"No unrelated release actions run without approval.",
	}
}
