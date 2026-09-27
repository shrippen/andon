package notify

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/services/hints"
)

// A hint in a maintenance window waits, even a critical one; afterwards
// it is pushed like any other.
func TestDueWaitsForMaintenance(t *testing.T) {
	now := time.Now()
	h := hints.View{Severity: enums.SeverityCritical, Maintenance: true}
	if due(h, time.Time{}, windowOpen, 0, now) {
		t.Fatal("pushed during maintenance")
	}
	h.Maintenance = false
	if !due(h, time.Time{}, windowOpen, 0, now) {
		t.Fatal("not pushed after maintenance")
	}
}
