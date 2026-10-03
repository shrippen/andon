package outbound_test

import (
	"context"
	"io"
	"net/http"
	"testing"
	"time"

	"andon/internal/outbound"
)

// TestDNSPauseAdGuard: AdGuard gets protection off with a duration in ms
// and may answer in plain text.
func TestDNSPauseAdGuard(t *testing.T) {
	var f fake
	to := f.serve(t, func(w http.ResponseWriter, _ call) { _, _ = io.WriteString(w, "OK") })
	if err := outbound.DNSPause(context.Background(), to, outbound.DNSAdGuard, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	c := f.one(t)
	body := c.json(t)
	if c.Method != http.MethodPost || c.Path != "/control/protection" || body["enabled"] != false || body["duration"] != 600000.0 {
		t.Fatalf("call %s %s %v", c.Method, c.Path, body)
	}
}

// TestDNSPausePihole: Pi-hole v6 logs in, sets a blocking timer, logs out.
func TestDNSPausePihole(t *testing.T) {
	var f fake
	to := f.serve(t, func(w http.ResponseWriter, c call) {
		if c.Path == "/api/auth" && c.Method == http.MethodPost {
			answer(http.StatusOK, `{"session": {"valid": true, "sid": "s1"}}`)(w, c)
			return
		}
		answer(http.StatusOK, `{}`)(w, c)
	})
	if err := outbound.DNSPause(context.Background(), to, outbound.DNSPihole, 10*time.Minute); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != 3 || f.calls[1].Path != "/api/dns/blocking" || f.calls[1].Header.Get("X-FTL-SID") != "s1" || f.calls[2].Method != http.MethodDelete {
		t.Fatalf("calls: %+v", f.calls)
	}
	if body := f.calls[1].json(t); body["blocking"] != false || body["timer"] != 600.0 {
		t.Fatalf("body: %v", body)
	}
}
