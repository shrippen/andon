package widgets

import (
	"testing"
	"time"

	"andon/internal/sources"
)

func TestDayBarWidensForEarlyWork(t *testing.T) {
	day := time.Date(2026, 9, 26, 0, 0, 0, 0, time.Local)
	at := func(h, m int) time.Time { return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute) }
	spans := []sources.KimaiSpan{{Begin: at(7, 30), End: at(9, 0)}, {Begin: at(13, 0)}}

	from, to, segs, now := dayBar(spans, at(15, 0))
	if from != 7 || to != 21 || len(segs) != 2 || segs[0].L < 3.5 || segs[0].L > 3.6 || !segs[1].Run || now != 57.14285714285714 {
		t.Fatalf("from %d to %d segs %+v now %v", from, to, segs, now)
	}
}

func TestClockSeconds(t *testing.T) {
	if got := clockSeconds(88*time.Minute + 16*time.Second); got != "1:28:16" {
		t.Fatalf("got %q", got)
	}
}

// Pinned pairs come first and leave the recent list; the running pair
// shows in neither.
func TestTimerViewFavs(t *testing.T) {
	live := &sources.KimaiLive{
		Active: []sources.KimaiTimer{{ID: 9, ProjectID: 1, ActivityID: 1, Begin: time.Now()}},
		Recent: []sources.KimaiTimer{{ProjectID: 1, ActivityID: 1}, {ProjectID: 2, ActivityID: 2}, {ProjectID: 3, ActivityID: 3}},
	}
	favs := []KimaiFav{{ProjectID: 1, ActivityID: 1}, {ProjectID: 2, ActivityID: 2, Project: "Shop"}}
	v := timerView(KimaiLiteConfig{Recent: 4}, map[string]any{"live": live, KimaiFavsPref: favs}, ViewCtx{})

	lists := v["Lists"].([]TimerList)
	if len(lists) != 2 || len(lists[0].Rows) != 1 || lists[0].Rows[0].Project != "Shop" || !lists[0].Rows[0].Pinned {
		t.Fatalf("favs: %+v", lists)
	}
	if len(lists[1].Rows) != 1 || lists[1].Rows[0].ProjectID != 3 || !v["Running"].(TimerRow).Pinned {
		t.Fatalf("recent: %+v running %+v", lists[1], v["Running"])
	}
}

// Stored favorites come back from JSON as []any.
func TestKimaiFavsOf(t *testing.T) {
	raw := []any{map[string]any{"project_id": 3.0, "activity_id": 7.0, "project": "Relaunch"}}
	if f := KimaiFavsOf(raw); len(f) != 1 || f[0].ProjectID != 3 || f[0].Project != "Relaunch" {
		t.Fatalf("favs: %+v", f)
	}
}

// Day rows: a running sheet has no end and counts up to now.
func TestDayRows(t *testing.T) {
	zone := time.FixedZone("", 2*3600)
	at := func(h, m int) time.Time { return time.Date(2026, 9, 29, h, m, 0, 0, zone) }
	day := &sources.KimaiDay{Sheets: []sources.KimaiTimer{{ID: 5, Begin: at(9, 0), End: at(12, 0)}, {ID: 6, Begin: at(13, 0)}}}

	rows := DayRows(day, at(13, 47))
	if rows[0].From != "09:00" || rows[0].To != "12:00" || rows[0].Duration != "3:00" {
		t.Fatalf("stopped: %+v", rows[0])
	}
	if rows[1].To != "" || rows[1].Duration != "0:47" {
		t.Fatalf("running: %+v", rows[1])
	}
}
