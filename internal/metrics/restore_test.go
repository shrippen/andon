package metrics

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// The sample stays for a quarter and moves on with the next.
func TestRestoreSample(t *testing.T) {
	borg := &sources.BorgDataset{Clients: []sources.BorgClient{{Name: "regis"}, {Name: "eredin"}, {Name: "ploetze"}}}
	q4 := RestoreSample(borg, time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC))
	if q4 == "" || q4 != RestoreSample(borg, time.Date(2026, 12, 30, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("q4: %q", q4)
	}
	if q1 := RestoreSample(borg, time.Date(2027, 1, 5, 0, 0, 0, 0, time.UTC)); q1 == q4 {
		t.Errorf("next quarter keeps %q", q1)
	}
	if RestoreSample(&sources.PGBackDataset{}, time.Now()) != "" {
		t.Error("sample without backups")
	}
}
