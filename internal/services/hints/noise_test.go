package hints_test

import (
	"path/filepath"
	"testing"
	"time"

	"andon/internal/crypto"
	"andon/internal/db"
	"andon/internal/db/dbtest"
	"andon/internal/enums"
	"andon/internal/repos/data"
	"andon/internal/rules"
	"andon/internal/services/hints"
)

// TestNoise: a hint that resolves and reopens on its own counts as
// coming and going; new hints are counted per day.
func TestNoise(t *testing.T) {
	crypto.Init("test-master-key")
	d, err := db.Open(filepath.Join(t.TempDir(), "n.db"), dbtest.Key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	who := person(t, d, "a@x.de")
	sid := ownSpace(who)

	rule := []string{"freshrss.stale_feed"}
	f := []rules.Finding{{Fingerprint: "stale", Rule: rule[0], Severity: enums.SeverityInfo, Message: "freshrss.stale"}}
	for range 3 {
		if _, err := hints.Sync(d, sid, nil, nil, rule, f); err != nil {
			t.Fatal(err)
		}
		if _, err := hints.Sync(d, sid, nil, nil, rule, nil); err != nil {
			t.Fatal(err)
		}
	}

	n, err := hints.Noise(d, who, time.Now().UTC(), 14)
	if err != nil {
		t.Fatal(err)
	}
	if len(n.Daily) != 14 || n.Daily[13] != 3 {
		t.Fatalf("daily: %v", n.Daily)
	}
	if len(n.Flapping) != 1 || n.Flapping[0].Rule != rule[0] || n.Flapping[0].Returns != 2 {
		t.Fatalf("flapping: %+v", n.Flapping)
	}
}

// TestOpenPerDay: open hints per level and day, from when each appeared
// to when it resolved; today counts what is open now.
func TestOpenPerDay(t *testing.T) {
	crypto.Init("test-master-key")
	d, err := db.Open(filepath.Join(t.TempDir(), "o.db"), dbtest.Key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	who := person(t, d, "a@x.de")
	sid := ownSpace(who)
	ids := []string{"kimai.missing_day"}
	crit := finding("crit")
	crit.Severity = enums.SeverityCritical
	if _, err := hints.Sync(d, sid, nil, nil, ids, []rules.Finding{crit, finding("info")}); err != nil {
		t.Fatal(err)
	}
	for _, fp := range []string{"crit", "info"} {
		h, _ := data.HintByPrint(d, sid, nil, fp)
		backdate(t, d, h.ID, 3)
	}
	// The info hint resolves today; the critical one stays.
	if _, err := hints.Sync(d, sid, nil, nil, ids, []rules.Finding{crit}); err != nil {
		t.Fatal(err)
	}

	n, err := hints.Noise(d, who, time.Now().UTC(), 5)
	if err != nil {
		t.Fatal(err)
	}
	// Days: -4 -3 -2 -1 today
	if got := n.Open[enums.SeverityCritical]; len(got) != 5 || got[0] != 0 || got[1] != 1 || got[4] != 1 {
		t.Fatalf("critical: %v", got)
	}
	if got := n.Open[enums.SeverityInfo]; got[1] != 1 || got[3] != 1 || got[4] != 0 {
		t.Fatalf("info: %v", got)
	}
}
