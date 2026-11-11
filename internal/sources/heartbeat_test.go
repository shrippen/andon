package sources_test

import (
	"context"
	"testing"

	"andon/internal/sources"
)

type fakeBeats []sources.Heartbeat

func (f fakeBeats) Heartbeats() []sources.Heartbeat { return f }

// TestHeartbeatsOf: every heartbeat source among the datasets counts,
// ordered by service key; other datasets are ignored; two connections
// of Healthchecks merge into one.
func TestHeartbeatsOf(t *testing.T) {
	a := &sources.HealthchecksDataset{Checks: []sources.Heartbeat{{Name: "a"}}}
	b := &sources.HealthchecksDataset{Checks: []sources.Heartbeat{{Name: "b"}}}
	merged := a.Merge(b).(*sources.HealthchecksDataset)
	if len(merged.Checks) != 2 || len(a.Checks) != 1 {
		t.Fatalf("merge %+v, a %+v", merged.Checks, a.Checks)
	}

	got := sources.HeartbeatsOf(map[string]any{
		"healthchecks": merged,
		"cronrunner":   fakeBeats{{Name: "c"}},
		"borgbackup":   &sources.BorgDataset{},
	})
	var names string
	for _, h := range got {
		names += h.Name
	}
	if names != "cab" {
		t.Fatalf("order %q", names)
	}
}

// TestHeartbeatReason: Healthchecks calls a down or late check missed,
// a healthy one has no reason; the link goes to the check's page when
// the uuid is known, else to the instance.
func TestHeartbeatReason(t *testing.T) {
	srv := jsonServer(t, map[string]any{"/api/v3/checks/": map[string]any{"checks": []map[string]any{
		{"name": "down", "status": "down", "uuid": "abc"},
		{"name": "late", "status": "grace"},
		{"name": "fine", "status": "up"},
	}}}, nil)
	raw, err := sources.HealthchecksData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "k"})
	if err != nil {
		t.Fatal(err)
	}
	beats := raw.(sources.HeartbeatSource).Heartbeats()
	if len(beats) != 3 {
		t.Fatalf("beats %+v", beats)
	}
	if beats[0].Reason != sources.HeartbeatMissed || beats[1].Reason != sources.HeartbeatMissed || beats[2].Reason != sources.HeartbeatNoReason {
		t.Fatalf("reasons %+v", beats)
	}
	if beats[0].Link != srv.URL+"/checks/abc/details/" || beats[1].Link != srv.URL || beats[0].Tool != "healthchecks" {
		t.Fatalf("links %+v", beats)
	}
}
