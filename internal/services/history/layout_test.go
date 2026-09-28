package history

import (
	"testing"
	"time"
)

// A new hint points at the update shortly before it; entries fall into
// days, newest first; the band counts events per slot.
func TestTimelineLayout(t *testing.T) {
	at := time.Date(2026, 9, 28, 9, 12, 0, 0, time.UTC)
	entries := []Entry{
		{At: at, Kind: "opened", Subject: "3 Rechnungen überfällig"},
		{At: at.Add(-4 * time.Minute), Kind: "update", Subject: "Invoice Ninja", Detail: "5.10 → 5.11"},
		{At: at.Add(-7 * time.Hour), Kind: "resolved", Subject: "Ungenutzt", Count: 54},
		{At: at.Add(-20 * time.Hour), Kind: "opened", Subject: "Pool DEGRADED"},
	}
	linkCauses(entries)
	if c := entries[0].Cause; c == nil || c.Subject != "Invoice Ninja" || entries[0].CauseMin != 4 || entries[3].Cause != nil {
		t.Fatalf("causes: %+v", entries)
	}

	days := ByDay(entries, time.UTC)
	if len(days) != 2 || len(days[0].Entries) != 3 || !days[1].Date.Equal(time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("days: %+v", days)
	}

	band := Band(entries, at.Add(-48*time.Hour), at, 6*time.Hour)
	if len(band) != 8 || band[len(band)-1].N != 2 {
		t.Fatalf("band: %+v", band)
	}
	var total int
	for _, b := range band {
		total += b.N
	}
	if total != 57 {
		t.Fatalf("band total %d", total)
	}
}
