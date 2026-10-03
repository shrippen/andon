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
