package dispatch

import (
	"testing"

	"github.com/thejorgg/taskforce/internal/domain"
)

func TestDispatchClassifiesAndStabilizesID(t *testing.T) {
	signal := domain.Signal{Source: "test", Content: "Critical crash on login"}
	first := Dispatcher{}.Dispatch(signal)
	second := Dispatcher{}.Dispatch(signal)
	if first.ID != second.ID {
		t.Fatalf("IDs differ: %s != %s", first.ID, second.ID)
	}
	if first.Category != "bug" {
		t.Fatalf("category = %q", first.Category)
	}
	if first.Severity != "critical" {
		t.Fatalf("severity = %q", first.Severity)
	}
	if !first.Actionable {
		t.Fatal("expected actionable task")
	}
}
