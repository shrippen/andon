package sources

import "testing"

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
