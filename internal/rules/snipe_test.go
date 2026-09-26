package rules_test

import (
	"strings"
	"testing"
	"time"

	"andon/internal/sources"
)

// TestSnipeUnusedIsOneHint: own gear lies ready for months as a matter of
// course; one hint sums it up (oldest first, same names counted) instead
// of one per patch cable.
func TestSnipeUnusedIsOneHint(t *testing.T) {
	env := todayEnv(nil)
	ago := func(days int) string { return env.Today.AddDate(0, 0, -days).Format(time.DateOnly) }
	data := &sources.SnipeDataset{URL: "https://snipe"}
	for i := range 10 {
		data.Assets = append(data.Assets, sources.SnipeAsset{ID: int64(i + 1), Name: "Patchkabel", Deployable: true, LastChange: ago(100 + i)})
	}
	data.Assets = append(data.Assets,
		sources.SnipeAsset{ID: 20, Name: "Akku", Deployable: true, LastChange: ago(400)},
		sources.SnipeAsset{ID: 21, Name: "Stativ", Deployable: true, LastChange: ago(95)},
		sources.SnipeAsset{ID: 22, Name: "Laptop", Deployable: true, Assigned: true, LastChange: ago(300)},
		sources.SnipeAsset{ID: 23, Name: "Kamera", Deployable: true, LastChange: ago(10)},
	)

	got := run(t, "snipe.unassigned_deployable", data, env)
	if len(got) != 1 {
		t.Fatalf("expected one hint, got %d: %+v", len(got), got)
	}
	p := got[0].Params
	if p["count"] != 12 || p["oldest"] != 400 || !strings.HasPrefix(p["names"].(string), "Akku, Patchkabel (10×), Stativ") {
		t.Fatalf("params: %+v", p)
	}
}
