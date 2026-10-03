package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"andon/internal/drivers/httpclient"
	"andon/internal/drivers/services"
)

// TestMaskSecrets: credential values vanish, the rest of the line stays.
func TestMaskSecrets(t *testing.T) {
	for in, want := range map[string]string{
		"db password=hunter2 ok":           "db password=*** ok",
		`{"token": "abc.def"}`:             `{"token": "***"}`,
		"Authorization: Bearer eyJhbGciOi": "Authorization: ***",
		"listening on :3000":               "listening on :3000",
		"API_KEY=xyz&page=2":               "API_KEY=***&page=2",
	} {
		if got := MaskSecrets(in); got != want {
			t.Errorf("%q → %q, want %q", in, got, want)
		}
	}
}

// TestLogLines: Docker's multiplexed frames become plain lines.
func TestLogLines(t *testing.T) {
	frame := func(stream byte, text string) string {
		n := len(text)
		return string([]byte{stream, 0, 0, 0, byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}) + text
	}
	got := logLines(frame(1, "started\n") + frame(2, "token=abc failed\n"))
	if len(got) != 2 || got[0] != "started" || got[1] != "token=*** failed" {
		t.Fatalf("lines: %q", got)
	}
	if plain := logLines("tty line\n"); len(plain) != 1 || plain[0] != "tty line" {
		t.Fatalf("tty: %q", plain)
	}
}

// TestContainerLoad: CPU from the two readings, memory without cache, no
// limit when Docker reports the host's.
func TestContainerLoad(t *testing.T) {
	cpu, mem, limit := containerLoad(map[string]any{
		"cpu_stats":    map[string]any{"cpu_usage": map[string]any{"total_usage": 300.0}, "system_cpu_usage": 2000.0, "online_cpus": 4.0},
		"precpu_stats": map[string]any{"cpu_usage": map[string]any{"total_usage": 100.0}, "system_cpu_usage": 1000.0},
		"memory_stats": map[string]any{"usage": 300.0 * bytesPerMB, "stats": map[string]any{"inactive_file": 100.0 * bytesPerMB}, "limit": float64(1 << 60)},
	})
	if cpu != 80 || mem != 200 || limit != 0 {
		t.Fatalf("load: %v %v %v", cpu, mem, limit)
	}
}

// Two servers' daily volumes add up per day.
func TestParseSabStats(t *testing.T) {
	s := parseSabStats(map[string]any{"month": 3.0, "servers": map[string]any{
		"a": map[string]any{"month": 2.0, "daily": map[string]any{"2026-09-30": 1.0}},
		"b": map[string]any{"month": 1.0, "daily": map[string]any{"2026-09-30": 2.0, "2026-09-29": 4.0}},
	}})
	if s.Daily["2026-09-30"] != 3 || s.Daily["2026-09-29"] != 4 || s.Servers["a"] != 2 || s.Month != 3 {
		t.Fatalf("%+v", s)
	}
}

// A minimal history names the entity in its first entry only.
func TestParseHassHistory(t *testing.T) {
	h := parseHassHistory([]any{[]any{
		map[string]any{"entity_id": "sensor.t", "state": "21.0", "last_changed": "2026-10-01T08:00:00Z"},
		map[string]any{"state": "21.5", "last_changed": "2026-10-01T09:00:00Z"},
	}})
	if p := h.ByID["sensor.t"]; len(p) != 2 || p[1].State != "21.5" || p[1].At.Hour() != 9 {
		t.Fatalf("%+v", h)
	}
}

// Days come out sorted, each currency as one line.
func TestParseRatesHistory(t *testing.T) {
	h := parseRatesHistory(map[string]any{"2026-09-30": map[string]any{"USD": 1.2}, "2026-09-29": map[string]any{"USD": 1.1}})
	if len(h.Days) != 2 || h.Days[0] != "2026-09-29" || h.ByCode["USD"][1] != 1.2 {
		t.Fatalf("%+v", h)
	}
}

// A red run names its first failed job and step.
func TestFailedStep(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"jobs": [{"name": "lint", "conclusion": "success", "steps": []},
			{"name": "test", "conclusion": "failure", "steps": [{"name": "checkout", "conclusion": "success"}, {"name": "go test", "conclusion": "failure"}]}]}`))
	}))
	defer srv.Close()
	api := services.BearerApi(srv.URL, "", httpclient.TLSVerify)
	if got := failedStep(context.Background(), api, "repos/a/b/actions/runs/7/jobs"); got != "test › go test" {
		t.Fatalf("got %q", got)
	}
}
