package sources

import (
	"encoding/json"
	"testing"
)

// TestBorgMetrics: a client gets its newest successful run and the size
// of all its repositories.
func TestBorgMetrics(t *testing.T) {
	data := &BorgDataset{Clients: []BorgClient{{Name: "nas"}}}
	var m any
	_ = json.Unmarshal([]byte(`{"plans": [
		{"client_id": 4, "client": "nas", "last_success_ts": 100, "last_success_bytes": 10, "last_success_duration_seconds": 60},
		{"client_id": 4, "client": "nas", "last_success_ts": 200, "last_success_bytes": 30, "last_success_duration_seconds": 90}],
		"repositories": [{"client_id": 4, "size_bytes": 1000}, {"client_id": 4, "size_bytes": 500}, {"client_id": 9, "size_bytes": 7}]}`), &m)
	addBorgMetrics(data, m)
	c := data.Clients[0]
	if c.LastBytes != 30 || c.LastSeconds != 90 || c.RepoBytes != 1500 {
		t.Fatalf("client: %+v", c)
	}
}

// TestSpeedResults: failed runs are left out, results come oldest first
// in Mbit/s.
func TestSpeedResults(t *testing.T) {
	var body any
	_ = json.Unmarshal([]byte(`{"data": [
		{"created_at": "2026-09-27T20:00:00Z", "download_bits": 100000000, "upload_bits": 20000000, "ping": 12, "status": "completed"},
		{"created_at": "2026-09-27T21:00:00Z", "status": "failed"},
		{"created_at": "2026-09-27T08:00:00Z", "download_bits": 250000000, "upload_bits": 40000000, "ping": 9}]}`), &body)
	got := parseSpeedResults(body).List
	if len(got) != 2 || got[0].Down != 250 || got[1].Down != 100 || got[1].Up != 20 {
		t.Fatalf("results: %+v", got)
	}
}
