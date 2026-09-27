package widgets

// "today": the day on one line of time, from what Andon already knows:
//
//	09:00  Daily Muster GmbH            (calendar connection)
//	10:12  Timer läuft · Relaunch       (Kimai)
//	14:05  S1 → Flughafen               (next departures, optional stop)
//	  5 T  USt-Voranmeldung 08/2026     (tax deadlines of the week)

import (
	"sort"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// TodayConfig is the "today" widget's config.
type TodayConfig struct {
	Timezone string
	Stop     string          // transit stop; "" = no departures
	Days     int             // deadlines this many days ahead
	Hide     map[string]bool // parts left out: event, timer, transit, deadline
	HidePast bool            // drop what is over instead of dimming it
}

// todayParts maps each part to its checkbox.
var todayParts = map[string]string{"event": "show_calendar", "timer": "show_timer", "transit": "show_transit", "deadline": "show_deadlines"}

const (
	todayDeadlineDays = 7
	todayDepartures   = 2
	peerCalendar      = "calendar"
)

func decodeToday(raw map[string]any) any {
	tz := asString(raw["timezone"])
	if tz == "" {
		tz = defaultTimezone
	}
	cfg := TodayConfig{Timezone: tz, Stop: asString(raw["stop"]), Days: clampInt(asInt(raw["days"], todayDeadlineDays), 1, 60),
		Hide: map[string]bool{}, HidePast: asBool(raw["hide_past"])}
	for part, box := range todayParts {
		if !boolOr(raw[box], true) {
			cfg.Hide[part] = true
		}
	}
	return cfg
}

// TodayItem is one entry of the day.
type TodayItem struct {
	Kind string // event, timer, transit, deadline
	At   string // "14:05", "" for all-day
	Text string
	Left int // deadline: days left
	// Deadline: its kind ("vat_return", "prepayment", "annual"), period, year.
	Deadline, Period string
	Year             int
	Past             bool // before now
	Now              bool // running (timer)
	at               time.Time
}

func todayQueries(cfgAny any) []Query {
	cfg := cfgAny.(TodayConfig)
	var q []Query
	if !cfg.Hide["event"] {
		q = append(q, peer(peerCalendar, enums.ServiceCalendar))
	}
	if !cfg.Hide["timer"] {
		q = append(q, kimaiPeer)
	}
	if cfg.Stop != "" && !cfg.Hide["transit"] {
		q = append(q, Query{Name: "board", Source: "transit", Params: map[string]any{"stop": cfg.Stop, "results": float64(todayDepartures)}})
	}
	return q
}

func todayView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg := cfgAny.(TodayConfig)
	loc, err := time.LoadLocation(cfg.Timezone)
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	y, m, d := now.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	clock := func(t time.Time) string { return t.In(loc).Format(timeOfDay) }

	var items []TodayItem
	if cal, ok := results[peerCalendar].(*sources.CalendarResult); ok {
		for _, e := range cal.Events {
			if e.Start.Before(start) || !e.Start.Before(end) {
				continue
			}
			item := TodayItem{Kind: "event", Text: e.Title, at: e.Start, Past: e.Start.Before(now)}
			if cfg.HidePast && item.Past && !e.AllDay {
				continue
			}
			if !e.AllDay {
				item.At = clock(e.Start)
			}
			items = append(items, item)
		}
	}
	if kimai, ok := results[peerKimai].(*sources.KimaiDataset); ok {
		for _, s := range kimai.Active {
			if begin, ok := metrics.ParseTime(s.Begin); ok {
				items = append(items, TodayItem{Kind: "timer", At: clock(begin), Text: s.Activity, at: begin, Now: true})
			}
		}
	}
	if board, ok := results["board"].(*sources.BoardResult); ok {
		for i, mv := range board.Movements {
			if i >= todayDepartures {
				break
			}
			items = append(items, TodayItem{Kind: "transit", At: clock(mv.When), Text: mv.Line + " → " + mv.Place, at: mv.When})
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].at.Before(items[j].at) })

	if tax, ok := metrics.ParseTaxSettings(ctx.Settings); ok && !cfg.Hide["deadline"] {
		for _, dl := range metrics.UpcomingDeadlines(tax, parseToday(ctx.Today), cfg.Days) {
			items = append(items, TodayItem{Kind: "deadline", Deadline: dl.Kind, Period: dl.Period, Year: dl.Year,
				Left: int(dl.Due.Sub(parseToday(ctx.Today)).Hours() / hoursPerDay)})
		}
	}
	return map[string]any{"Items": items, "Now": clock(now)}
}

func init() {
	Register(WidgetType{Key: "today", Decode: decodeToday, Template: "widgets/today", Category: CategoryInsight,
		RefreshS: 300, Queries: todayQueries, View: todayView})
}
