package sources_test

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// TestDemoDaysFollowUTC: demo dates count from the UTC day, like the
// rules' Today; at 00:30 in Berlin (22:30 UTC the day before) the demo
// used the local day and the Paperless inbox was a day younger.
func TestDemoDaysFollowUTC(t *testing.T) {
	berlin := time.FixedZone("CEST", 2*60*60)
	now := time.Date(2026, 9, 27, 0, 30, 0, 0, berlin)
	if got, want := sources.DemoPaperless(now).OldestAdded, "2026-09-03"; got != want {
		t.Fatalf("oldest added %s, want %s (23 days before the UTC day 2026-09-26)", got, want)
	}
}
