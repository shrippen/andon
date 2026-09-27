package widgets_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// TestArrDays: only what comes within the chosen days.
func TestArrDays(t *testing.T) {
	now := time.Now()
	data := &sources.ArrDataset{Upcoming: []sources.ArrItem{{Title: "soon", At: now.Add(24 * time.Hour)}, {Title: "later", At: now.AddDate(0, 0, 20)}}}
	v := viewOf(t, "arr_upcoming", map[string]any{"days": 3.0}, map[string]any{"data": data}, enums.ServiceArr, nil)
	if items := v["Items"].([]sources.ArrItem); len(items) != 1 || items[0].Title != "soon" {
		t.Fatalf("arr: %+v", items)
	}
}

// TestSabQueue: the first queue entries.
func TestSabQueue(t *testing.T) {
	v := viewOf(t, "sabnzbd", map[string]any{"queue": 2.0}, map[string]any{"data": sources.DemoSabnzbd(time.Now())}, enums.ServiceSabnzbd, nil)
	if q := v["Queue"].([]sources.SabItem); len(q) != 2 {
		t.Fatalf("queue: %+v", q)
	}
}

// TestPaperlessTag: another tag's count and the newest documents.
func TestPaperlessTag(t *testing.T) {
	v := viewOf(t, "paperless_inbox", map[string]any{"tag": "Belege", "newest_docs": 2.0}, map[string]any{"data": sources.DemoPaperless(time.Now())}, enums.ServicePaperless, nil)
	if v["Count"] != 212 || len(v["Newest"].([]sources.PaperlessNew)) != 2 {
		t.Fatalf("paperless: %+v", v)
	}
}

// TestFreshRSSFilter: one category, feeds without unread included.
func TestFreshRSSFilter(t *testing.T) {
	data := &sources.FreshRSSDataset{Unread: 9, Feeds: []sources.Feed{{Title: "Heise", Category: "Tech", Unread: 5}, {Title: "Golem", Category: "Tech"}, {Title: "Kochen", Category: "Privat", Unread: 4}}}
	v := viewOf(t, "freshrss_feeds", map[string]any{"filter": []any{"tech"}, "only_unread": false}, map[string]any{"data": data}, enums.ServiceFreshRSS, nil)
	if v["Unread"] != 5 || v["Feeds"] != 2 || len(v["Bars"].([]widgets.HBar)) != 2 {
		t.Fatalf("freshrss: %+v", v)
	}
}

// TestLinkwardenNewest: the latest link first.
func TestLinkwardenNewest(t *testing.T) {
	v := viewOf(t, "linkwarden", map[string]any{"newest": true}, map[string]any{"data": sources.DemoLinkwarden()}, enums.ServiceLinkwarden, nil)
	if links := v["Newest"].([]sources.Bookmark); len(links) != 3 || links[0].Name != "Grafana" {
		t.Fatalf("links: %+v", links)
	}
}

// TestMediaUsers: names can stay off a wall display.
func TestMediaUsers(t *testing.T) {
	v := viewOf(t, "mediaserver", map[string]any{"show_users": false}, map[string]any{"data": &sources.MediaServerDataset{}}, enums.ServiceMediaServer, nil)
	if v["Users"] != false {
		t.Fatalf("media: %+v", v)
	}
}

// TestHassLabelsAndThresholds: an own name, and a threshold on it.
func TestHassLabelsAndThresholds(t *testing.T) {
	data := &sources.HassDataset{Entities: []sources.Entity{{ID: "sensor.office", Name: "Office temp", State: "27.5", Domain: "sensor"}}}
	v := viewOf(t, "hass", map[string]any{"entities": []any{"sensor.office"}, "labels": "sensor.office = Büro", "thresholds": "Büro > 26 gelb", "two_columns": true},
		map[string]any{"data": data}, enums.ServiceHomeAssistant, nil)
	rows := v["Rows"].([]widgets.HassRow)
	if rows[0].Name != "Büro" || rows[0].Level != "warn" || v["TwoCols"] != true {
		t.Fatalf("hass: %+v", v)
	}
}

// TestGrocyParts: only the shopping list.
func TestGrocyParts(t *testing.T) {
	data := &sources.GrocyDataset{Expired: []sources.Product{{Name: "Milch"}}, Missing: []sources.Product{{Name: "Kaffee"}}, Chores: []sources.Chore{{Name: "Bad"}}}
	v := viewOf(t, "grocy", map[string]any{"show_stock": false, "show_chores": false}, map[string]any{"data": data}, enums.ServiceGrocy, nil)
	shown := v["Data"].(*sources.GrocyDataset)
	if len(shown.Expired) != 0 || len(shown.Chores) != 0 || len(shown.Missing) != 1 || len(data.Expired) != 1 {
		t.Fatalf("grocy: %+v", shown)
	}
}

// TestCalendarMerge: two calendars in time order, each with its colour,
// all-day events left out.
func TestCalendarMerge(t *testing.T) {
	now := time.Now()
	a := &sources.CalendarResult{Events: []sources.Event{{Title: "A later", Start: now.Add(3 * time.Hour)}, {Title: "Holiday", Start: now, AllDay: true}}}
	b := &sources.CalendarResult{Events: []sources.Event{{Title: "B first", Start: now.Add(time.Hour)}}}
	raw := map[string]any{"ical_url": "https://a.example/x.ics", "ical_url_2": "https://b.example/y.ics", "color_2": "blue", "hide_all_day": true}
	cfg, _ := widgets.Decode("calendar", raw)
	kind, _ := widgets.Get("calendar")
	if q := kind.Queries(cfg); len(q) != 2 || q[1].Name != "events2" {
		t.Fatalf("queries: %+v", q)
	}
	v := viewOf(t, "calendar", raw, map[string]any{"events": a, "events2": b}, "", nil)
	rows := v["Rows"].([]widgets.CalRow)
	if len(rows) != 2 || rows[0].Title != "B first" || rows[0].Color != "blue" || rows[1].Color != "none" {
		t.Fatalf("rows: %+v", rows)
	}
}

// TestHolidayBridge: a Thursday holiday names the Friday.
func TestHolidayBridge(t *testing.T) {
	days := &sources.HolidaysResult{Days: []sources.Holiday{{Day: "2026-10-01", Name: "Test"}, {Day: "2026-10-03", Name: "Einheit"}}}
	v := viewOf(t, "holidays", map[string]any{"bridges": true}, map[string]any{"days": days}, "", nil)
	rows := v["Rows"].([]widgets.HolidayRow)
	if rows[0].Bridge != "2026-10-02" || rows[1].Bridge != "" {
		t.Fatalf("rows: %+v", rows)
	}
}

// TestWeatherFahrenheit: °F, no hours, two days ahead.
func TestWeatherFahrenheit(t *testing.T) {
	w := &sources.WeatherResult{Temp: 20, Days: []sources.WeatherDay{{Max: 20}, {Max: 10}, {Max: 0}, {Max: 30}},
		Hours: []sources.WeatherHour{{Temp: 1}, {Temp: 2}}}
	v := viewOf(t, "weather", map[string]any{"unit": "f", "hourly": false, "days": 2.0}, map[string]any{"weather": w}, "", nil)
	days := v["Days"].([]sources.WeatherDay)
	if v["Temp"] != 68.0 || len(days) != 2 || days[0].Max != 50 || v["Spark"] != nil || w.Days[1].Max != 10 {
		t.Fatalf("weather: %+v", v)
	}
}

// TestTransitWalk: lines filtered, what leaves before one could walk
// there left out.
func TestTransitWalk(t *testing.T) {
	now := time.Now()
	board := &sources.BoardResult{Movements: []sources.Movement{{Line: "S 1", When: now.Add(2 * time.Minute)}, {Line: "S1", When: now.Add(15 * time.Minute)},
		{Line: "U2", When: now.Add(20 * time.Minute)}}}
	v := viewOf(t, "transit", map[string]any{"stop": "x", "lines": []any{"s1"}, "walk": 5.0}, map[string]any{"board": board}, "", nil)
	if rows := v["Rows"].([]widgets.MoveRow); len(rows) != 1 {
		t.Fatalf("transit: %+v", rows)
	}
}

// TestFlightsAirline: only Lufthansa.
func TestFlightsAirline(t *testing.T) {
	board := &sources.BoardResult{Movements: []sources.Movement{{Line: "LH 123", When: time.Now()}, {Line: "EW 9", When: time.Now()}}}
	v := viewOf(t, "flights", map[string]any{"airport": "HAM", "airlines": []any{"lh"}}, map[string]any{"board": board}, "", nil)
	if rows := v["Rows"].([]widgets.MoveRow); len(rows) != 1 || rows[0].Line != "LH 123" {
		t.Fatalf("flights: %+v", rows)
	}
}

// TestDWDLevel: minor warnings drop out.
func TestDWDLevel(t *testing.T) {
	data := &sources.DWDDataset{Warnings: []sources.WeatherWarning{{Severity: "minor"}, {Severity: "severe"}}}
	v := viewOf(t, "dwd", map[string]any{"min_level": "moderate"}, map[string]any{"data": data}, enums.ServiceDWD, nil)
	if w := v["Data"].(*sources.DWDDataset).Warnings; len(w) != 1 || w[0].Severity != "severe" {
		t.Fatalf("dwd: %+v", w)
	}
}
