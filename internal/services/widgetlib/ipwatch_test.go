package widgetlib_test

import (
	"testing"
	"time"

	"andon/internal/services/access"
	"andon/internal/services/widgetlib"
)

// The first address is only remembered; a new one keeps the old one and
// the time of the change.
func TestWatchIP(t *testing.T) {
	d := openTestDB(t)
	u := addUser(t, d, "ip@b.c")
	who, _ := access.Load(d, u.ID)
	now := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)

	seen, err := widgetlib.WatchIP(d, who, "203.0.113.5", now)
	if err != nil || seen.Prev != "" || !seen.Since.IsZero() {
		t.Fatalf("first: %+v %v", seen, err)
	}
	seen, _ = widgetlib.WatchIP(d, who, "203.0.113.5", now.Add(time.Hour))
	if seen.Prev != "" {
		t.Fatalf("same address: %+v", seen)
	}
	seen, _ = widgetlib.WatchIP(d, who, "198.51.100.9", now.Add(2*time.Hour))
	if seen.Prev != "203.0.113.5" || !seen.Since.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("changed: %+v", seen)
	}
	seen, _ = widgetlib.WatchIP(d, who, "198.51.100.9", now.Add(3*time.Hour))
	if seen.Prev != "203.0.113.5" || !seen.Since.Equal(now.Add(2*time.Hour)) {
		t.Fatalf("kept: %+v", seen)
	}
}
