package echo

import (
	"crypto/sha1"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/thejorgg/taskforce/internal/domain"
)

type Collector struct {
	Now func() time.Time
}

func (c Collector) FromText(source, content string, artifacts []string) domain.Signal {
	now := time.Now
	if c.Now != nil {
		now = c.Now
	}
	content = strings.TrimSpace(content)
	if source == "" {
		source = "cli"
	}
	id := signalID(source, content)
	return domain.Signal{
		ID:        id,
		Source:    source,
		Content:   content,
		Artifacts: artifacts,
		CreatedAt: now(),
	}
}

func (c Collector) FromFile(path string) (domain.Signal, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return domain.Signal{}, err
	}
	return c.FromText("file:"+path, string(data), []string{path}), nil
}

func signalID(source, content string) string {
	sum := sha1.Sum([]byte(source + "\x00" + content))
	return fmt.Sprintf("sig-%x", sum[:6])
}
