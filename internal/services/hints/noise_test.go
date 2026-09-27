package hints_test

import (
	"path/filepath"
	"testing"
	"time"

	"andon/internal/crypto"
	"andon/internal/db"
	"andon/internal/db/dbtest"
	"andon/internal/enums"
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
