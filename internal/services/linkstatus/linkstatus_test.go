package linkstatus_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"andon/internal/crypto"
	"andon/internal/db"
	"andon/internal/db/dbtest"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/repos/data"
	"andon/internal/services/linkstatus"
)

func TestCheckBarsAndDownDays(t *testing.T) {
	crypto.Init(crypto.Derive("test-master-key", nil))
	d, err := db.Open(filepath.Join(t.TempDir(), "l.db"), dbtest.Key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer up.Close()

	sp := &model.Space{Kind: enums.SpacePersonal, Name: "x", Version: 1}
	if err := content.AddSpace(d, sp); err != nil {
		t.Fatal(err)
	}
	add := func(key, url string) int64 {
		w := &model.Widget{SpaceID: sp.ID, Key: key, Type: "link", Title: key, Version: 1, UpdatedAt: time.Now().UTC(),
			Config: map[string]any{"url": url, "status": "http"}}
		if err := content.AddWidget(d, w); err != nil {
			t.Fatal(err)
		}
		return w.ID
	}
	good, dead := add("good", up.URL), add("dead", "http://127.0.0.1:1")

	if err := linkstatus.Check(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC()
	bars, ok := linkstatus.Bars(d, good, today)
	if !ok || len(bars.Bars) != 30 || bars.Bars[29].State != linkstatus.BarUp || bars.Share != 1 {
		t.Fatalf("good: %+v", bars)
	}

	// Three earlier days without a single success, then today.
	for i := 1; i <= 3; i++ {
		if err := data.RecordStatus(d, dead, today.AddDate(0, 0, -i).Format("2006-01-02"), data.Check{}); err != nil {
			t.Fatal(err)
		}
	}
	if n := linkstatus.DownDays(d, dead, today); n != 4 {
		t.Fatalf("down days: %d", n)
	}
	if n := linkstatus.DownDays(d, good, today); n != 0 {
		t.Fatalf("good down days: %d", n)
	}
}

func TestHistoryDaysAndIncidents(t *testing.T) {
	crypto.Init(crypto.Derive("test-master-key", nil))
	d, err := db.Open(filepath.Join(t.TempDir(), "h.db"), dbtest.Key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	sp := &model.Space{Kind: enums.SpacePersonal, Name: "x", Version: 1}
	if err := content.AddSpace(d, sp); err != nil {
		t.Fatal(err)
	}
	w := &model.Widget{SpaceID: sp.ID, Key: "dead", Type: "link", Title: "dead", Version: 1, UpdatedAt: time.Now().UTC(),
		Config: map[string]any{"url": "http://127.0.0.1:1", "status": "http"}}
	if err := content.AddWidget(d, w); err != nil {
		t.Fatal(err)
	}

	// Today's check fails for real; before that: up 10 days ago, two
	// days with HTTP 502, one partly failing day.
	if err := linkstatus.Check(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	today := time.Now().UTC()
	day := func(back int) string { return today.AddDate(0, 0, -back).Format("2006-01-02") }
	record := func(back int, c data.Check) {
		if err := data.RecordStatus(d, w.ID, day(back), c); err != nil {
			t.Fatal(err)
		}
	}
	record(10, data.Check{Up: true, MS: 120})
	record(2, data.Check{Error: "HTTP 502"})
	record(1, data.Check{Error: "HTTP 502"})
	record(1, data.Check{Up: true, MS: 300})

	h := linkstatus.HistoryOf(d, w.ID, today)
	if len(h.Days) != 60 {
		t.Fatalf("days: %d", len(h.Days))
	}
	last := h.Days[59]
	if last.State != linkstatus.BarDown || last.Error == "" || last.Fail != 1 {
		t.Fatalf("today: %+v", last)
	}
	if got := h.Days[58]; got.State != linkstatus.BarPartial || got.Error != "HTTP 502" || got.AvgMs != 300 {
		t.Fatalf("yesterday: %+v", got)
	}
	if got := h.Days[49]; got.State != linkstatus.BarUp || got.AvgMs != 120 {
		t.Fatalf("10 days ago: %+v", got)
	}

	// One incident from two days ago to today, newest first.
	if len(h.Incidents) != 1 {
		t.Fatalf("incidents: %+v", h.Incidents)
	}
	inc := h.Incidents[0]
	if inc.Days != 3 || inc.Failed != 3 || !inc.Down || inc.Error != "HTTP 502" {
		t.Fatalf("incident: %+v", inc)
	}
	if h.Checks != 5 || h.Since.Format("2006-01-02") != day(10) {
		t.Fatalf("checks %d since %v", h.Checks, h.Since)
	}
}

// TrendOf lists the last 24 hours' mean response times, oldest first; an
// hour without a successful check has no point.
func TestTrend(t *testing.T) {
	crypto.Init(crypto.Derive("test-master-key", nil))
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"), dbtest.Key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	sp := &model.Space{Kind: enums.SpacePersonal, Name: "x", Version: 1}
	if err := content.AddSpace(d, sp); err != nil {
		t.Fatal(err)
	}
	w := &model.Widget{SpaceID: sp.ID, Key: "k", Type: "link", Title: "k", Version: 1, UpdatedAt: time.Now().UTC(),
		Config: map[string]any{"url": "http://x.example", "status": "http"}}
	if err := content.AddWidget(d, w); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 10, 4, 12, 30, 0, 0, time.UTC)
	if _, ok := linkstatus.TrendOf(d, w.ID, now); ok {
		t.Fatal("trend without checks")
	}

	record := func(ago time.Duration, c data.Check) {
		if err := data.RecordStatusHour(d, w.ID, now.Add(-ago).Format("2006-01-02T15"), c); err != nil {
			t.Fatal(err)
		}
	}
	record(3*time.Hour, data.Check{Up: true, MS: 100})
	record(3*time.Hour, data.Check{Up: true, MS: 200})
	record(2*time.Hour, data.Check{})
	record(0, data.Check{Up: true, MS: 90})
	record(30*time.Hour, data.Check{Up: true, MS: 999}) // outside the window

	tr, ok := linkstatus.TrendOf(d, w.ID, now)
	if !ok {
		t.Fatal("no trend")
	}
	if len(tr.Ms) != 2 || tr.Ms[0] != 150 || tr.Ms[1] != 90 || tr.Min != 90 || tr.Max != 150 {
		t.Fatalf("trend: %+v", tr)
	}
}
