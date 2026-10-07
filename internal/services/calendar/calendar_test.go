package calendar_test

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/rules"
	"andon/internal/services/access"
	"andon/internal/services/calendar"
	"andon/internal/services/hints"
	"andon/internal/services/spaces"
	"andon/internal/testkit"
)

// The feed is valid iCalendar with one all-day event per tax deadline.
func TestFeedListsTaxDeadlines(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	tax := map[string]any{"vat": map[string]any{"return_interval": "quarterly"}}
	if err := spaces.Update(d, who, space, map[string]any{"tax": tax}, ""); err != nil {
		t.Fatal(err)
	}
	who, _ = access.Load(d, who.UserID)

	feed, err := calendar.Feed(d, who, time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC), "https://andon.test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(feed, "BEGIN:VCALENDAR\r\n") || !strings.HasSuffix(feed, "END:VCALENDAR\r\n") {
		t.Fatalf("frame:\n%s", feed)
	}
	events := strings.Count(feed, "BEGIN:VEVENT")
	if events == 0 || events != strings.Count(feed, "DTSTART;VALUE=DATE:") {
		t.Fatalf("events:\n%s", feed)
	}
}

// Two hints with the same title and deadline are one event; it carries
// both reasons and a link back to Andon. Long lines fold at 75 octets.
func TestFeedMergesHints(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	due := "2026-10-20"
	long := strings.Repeat("ä", 60)
	findings := []rules.Finding{
		{Fingerprint: "a", Rule: "esphome.offline", Severity: enums.SeverityWarn, Message: "esphome.offline",
			Params: map[string]any{"name": "Pumpe", "address": "10.0.0.1"}, Due: due},
		{Fingerprint: "b", Rule: "esphome.offline", Severity: enums.SeverityWarn, Message: "esphome.offline",
			Params: map[string]any{"name": "Pumpe", "address": long}, Due: due},
	}
	err := db.WithTx(d, func(tx *sql.Tx) error {
		_, err := hints.Sync(tx, space, nil, nil, []string{"esphome.offline"}, findings)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	feed, err := calendar.Feed(d, who, time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), "https://andon.test/")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(feed, "BEGIN:VEVENT"); n != 1 {
		t.Fatalf("events %d, want 1:\n%s", n, feed)
	}
	for _, line := range strings.Split(strings.TrimSuffix(feed, "\r\n"), "\r\n") {
		if len(line) > 75 {
			t.Fatalf("line not folded (%d octets): %q", len(line), line)
		}
	}
	unfolded := strings.ReplaceAll(feed, "\r\n ", "")
	if !strings.Contains(unfolded, "\r\nURL:https://andon.test/hints#hint-") {
		t.Fatalf("no URL:\n%s", feed)
	}
	desc := ""
	for _, line := range strings.Split(unfolded, "\r\n") {
		if strings.HasPrefix(line, "DESCRIPTION:") {
			desc = line
		}
	}
	if !strings.Contains(desc, "10.0.0.1") || !strings.Contains(desc, long) {
		t.Fatalf("description %q", desc)
	}
}
