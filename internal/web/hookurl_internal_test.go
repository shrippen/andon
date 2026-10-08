package web

import "testing"

// TestUnreachable: a hook URL on the loopback cannot be called by a
// service on another host.
func TestUnreachable(t *testing.T) {
	for raw, want := range map[string]bool{
		"http://localhost:8080/hooks/1/x":   true,
		"http://127.0.0.1/hooks/1/x":        true,
		"http://[::1]:8080/hooks/1/x":       true,
		"https://andon.example/hooks/1/x":   false,
		"http://192.168.1.5:8080/hooks/1/x": false,
	} {
		if got := unreachable(raw); got != want {
			t.Errorf("%s: %v", raw, got)
		}
	}
}
