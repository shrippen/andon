package rules_test

import (
	"testing"
	"time"

	"andon/internal/sources"
)

func TestKintsugiRules(t *testing.T) {
	data := sources.DemoKintsugi(time.Now())
	if got := run(t, "kintsugi.run_failed", data, todayEnv(nil)); len(got) != 1 || got[0].Params["detail"] != "LLM nicht erreichbar" {
		t.Fatalf("run_failed: %+v", got)
	}
	// The oldest demo suggestion is 8 days old, over the 7-day default.
	if got := run(t, "kintsugi.stale", data, todayEnv(nil)); len(got) != 1 || got[0].Params["count"] != 3 {
		t.Fatalf("stale: %+v", got)
	}
	if got := run(t, "kintsugi.budget", data, todayEnv(nil)); len(got) != 0 {
		t.Fatalf("budget left but reported: %+v", got)
	}

	data.UsedUSD = data.BudgetUSD
	data.LastRun.Status = "ok"
	data.Open = data.Open[:2]
	if got := run(t, "kintsugi.budget", data, todayEnv(nil)); len(got) != 1 {
		t.Fatalf("budget: %+v", got)
	}
	if got := run(t, "kintsugi.run_failed", data, todayEnv(nil)); len(got) != 0 {
		t.Fatalf("ok run reported: %+v", got)
	}
	if got := run(t, "kintsugi.stale", data, todayEnv(nil)); len(got) != 0 {
		t.Fatalf("fresh suggestions reported: %+v", got)
	}
}

// TestKintsugiFingerprintsStay: the same state a few minutes later is the
// same hint, not a resolved one and a new one.
func TestKintsugiFingerprintsStay(t *testing.T) {
	now := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"kintsugi.run_failed", "kintsugi.stale"} {
		a := run(t, id, sources.DemoKintsugi(now), todayEnv(nil))
		b := run(t, id, sources.DemoKintsugi(now.Add(10*time.Minute)), todayEnv(nil))
		if len(a) != 1 || len(b) != 1 || a[0].Fingerprint != b[0].Fingerprint {
			t.Errorf("%s: fingerprints %v then %v", id, a, b)
		}
	}
}
