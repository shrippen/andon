package widgets

// kimai_timer ("Kimai Lite"): a small web version of the Plasmai panel
// widget on the Kimai connection.
//
//	┌──────────────────────────────────────┐
//	│ 01:28:16                      Intern │  running timer, ticks in the browser
//	│ Server/Dienste · Wartung             │
//	│ seit 13:04 · Heute 4:53 · Woche 26:30│
//	│ ▕██▁▁███▁▁▁▁▓▓▓│▁▁▁▁▁▁▏  09 … 21      │  today: booked, running, now
//	│ [Beschreibung…]              [Stopp] │
//	│ Favoriten ▶ Muster · Schnitt       ★ │  pinned pairs
//	│ Zuletzt  ▶ Muster · Website        ☆ │  start, or switch when running
//	└──────────────────────────────────────┘
//
// The head opens the add-entry form and today's list (edit, split,
// delete); the running entry has its own edit form.

import (
	"encoding/json"
	"fmt"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

const (
	dayBarFrom  = 9  // the day bar spans at least 09–21 …
	dayBarTo    = 21 // … and widens for earlier or later work
	dayBarTick  = 3  // hours between labels
	recentShown = 4
	recentMax   = 10 // what the source fetches
)

// KimaiLiteConfig is the "kimai_timer" widget's config.
// The weekly target comes from the Kimai work contract, never from here.
type KimaiLiteConfig struct {
	Recent  int  // quick-start rows
	AskNote bool // a description field for the timer being started
}

func decodeKimaiLite(raw map[string]any) any {
	return KimaiLiteConfig{Recent: clampInt(asInt(raw["recent"], recentShown), 0, recentMax), AskNote: asBool(raw["ask_note"])}
}

// TimerRow is one running or startable timer.
type TimerRow struct {
	ID, ProjectID, ActivityID   int64
	Project, Activity, Customer string
	Color                       string
	Begin                       string // RFC3339, for the ticking clock
	Since                       string // "13:04"
	Elapsed                     string // "1:28:16"
	Pinned                      bool
}

// TimerList is a titled group of startable rows (pinned, recent).
type TimerList struct {
	Label string // message key
	Rows  []TimerRow
}

// DaySeg is one booked or running block of the day bar, in percent.
type DaySeg struct {
	L, W float64
	Run  bool
}

// DayTick is one hour label of the day bar.
type DayTick struct {
	Hour int
	Pos  float64
}

// KimaiFavsPref is the user pref of pinned project-activity pairs per
// Kimai connection: {"12": [{"project_id": 3, "activity_id": 7, …}]}.
// The result slot of the same name holds the tile connection's list.
const KimaiFavsPref = "kimai_favs"

// KimaiFav is one pinned pair, names kept from when it was pinned.
type KimaiFav struct {
	ProjectID  int64  `json:"project_id"`
	ActivityID int64  `json:"activity_id"`
	Project    string `json:"project"`
	Activity   string `json:"activity"`
	Customer   string `json:"customer,omitempty"`
	Color      string `json:"color,omitempty"`
}

// KimaiFavsOf reads a stored list, which comes back from JSON as []any.
func KimaiFavsOf(raw any) []KimaiFav {
	var out []KimaiFav
	b, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	_ = json.Unmarshal(b, &out)
	return out
}

func timerRow(t sources.KimaiTimer) TimerRow {
	return TimerRow{ID: t.ID, ProjectID: t.ProjectID, ActivityID: t.ActivityID, Project: t.Project, Activity: t.Activity,
		Customer: t.Customer, Color: t.Color}
}

// clockMinutes: 67 → "1:07".
func clockMinutes(m int) string {
	return fmt.Sprintf("%d:%02d", m/minutesPerHour, m%minutesPerHour)
}

// clockSeconds: 1h28m16s → "1:28:16".
func clockSeconds(d time.Duration) string {
	s := int(max(d, 0).Seconds())
	return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
}

func timerView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, _ := cfgAny.(KimaiLiteConfig)
	data, ok := results["live"].(*sources.KimaiLive)
	if !ok {
		return map[string]any{}
	}
	now := time.Now()
	favs, _ := results[KimaiFavsPref].([]KimaiFav)
	pinned := map[[2]int64]bool{}
	for _, f := range favs {
		pinned[[2]int64{f.ProjectID, f.ActivityID}] = true
	}
	out := map[string]any{"URL": data.URL, "Today": clockMinutes(data.TodayMin), "Week": clockMinutes(data.WeekMin)}

	spans := data.Today
	running := map[[2]int64]bool{}
	if len(data.Active) > 0 {
		t := data.Active[0]
		row := timerRow(t)
		if !t.Begin.IsZero() {
			local := t.Begin.In(now.Location())
			row.Begin, row.Since, row.Elapsed = t.Begin.Format(time.RFC3339), local.Format("15:04"), clockSeconds(now.Sub(t.Begin))
			spans = append(spans, sources.KimaiSpan{Begin: t.Begin})
		}
		row.Pinned = pinned[[2]int64{t.ProjectID, t.ActivityID}]
		out["Running"] = row
		for _, a := range data.Active {
			running[[2]int64{a.ProjectID, a.ActivityID}] = true
		}
	}

	// Pinned pairs first, then recent ones that are neither running nor
	// pinned.
	var favRows, recent []TimerRow
	for _, f := range favs {
		if !running[[2]int64{f.ProjectID, f.ActivityID}] {
			favRows = append(favRows, TimerRow{ProjectID: f.ProjectID, ActivityID: f.ActivityID, Project: f.Project,
				Activity: f.Activity, Customer: f.Customer, Color: f.Color, Pinned: true})
		}
	}
	for _, t := range data.Recent {
		pair := [2]int64{t.ProjectID, t.ActivityID}
		if !running[pair] && !pinned[pair] && len(recent) < cfg.Recent {
			recent = append(recent, timerRow(t))
		}
	}
	out["Favs"], out["Recent"], out["AskNote"] = favRows, recent, cfg.AskNote
	var lists []TimerList
	for _, l := range []TimerList{{"timer.favs", favRows}, {"timer.recent", recent}} {
		if len(l.Rows) > 0 {
			lists = append(lists, l)
		}
	}
	out["Lists"] = lists

	if week := data.Contract.WeekMinutes(); week > 0 {
		left := week - data.WeekMin
		out["Target"], out["Left"], out["Over"] = float64(week)/minutesPerHour, clockMinutes(max(left, -left)), left < 0
	}

	from, to, segs, pos := dayBar(spans, now)
	out["DaySegs"], out["DayNow"], out["DayTicks"] = segs, pos, dayTicks(from, to)
	return out
}

// DayRow is one of today's timesheets in the Kimai Lite day list, in
// Kimai's own clock (the edit form's).
type DayRow struct {
	TimerRow
	From, To string // "09:05", "" while running
	Duration string // "2:35"
	Note     string
	Tags     []string
	Billable bool
}

// DayRows lists today's sheets, oldest first.
//
//	09:05–11:40  2:35  Muster · Schnitt
//	13:04–       0:47  Intern · Wartung   (running)
func DayRows(day *sources.KimaiDay, now time.Time) []DayRow {
	if day == nil {
		return nil
	}
	var out []DayRow
	for _, t := range day.Sheets {
		row := DayRow{TimerRow: timerRow(t), From: t.Begin.Format("15:04"), Note: t.Description, Tags: t.Tags, Billable: t.Billable}
		end := t.End
		if end.IsZero() {
			end = now
		} else {
			row.To = end.Format("15:04")
		}
		row.Duration = clockMinutes(int(max(end.Sub(t.Begin), 0).Minutes()))
		out = append(out, row)
	}
	return out
}

// dayBar places today's spans on an hour axis of at least 09–21:
//
//	spans 09:05–11:40, 13:04–now(14:32)  →  axis 9–21, blocks at 0.7 % and 34 %
func dayBar(spans []sources.KimaiSpan, now time.Time) (int, int, []DaySeg, float64) {
	from, to := dayBarFrom, dayBarTo
	hourOf := func(t time.Time) float64 {
		t = t.In(now.Location())
		return float64(t.Hour()) + float64(t.Minute())/minutesPerHour
	}
	for _, s := range spans {
		end := s.End
		if end.IsZero() {
			end = now
		}
		from = min(from, int(hourOf(s.Begin)))
		to = max(to, int(hourOf(end))+1)
	}
	to = min(to, 24)

	span := float64(to - from)
	pct := func(h float64) float64 { return min(max((h-float64(from))/span*pctFull, 0), pctFull) }
	var segs []DaySeg
	for _, s := range spans {
		end, run := s.End, s.End.IsZero()
		if run {
			end = now
		}
		l := pct(hourOf(s.Begin))
		segs = append(segs, DaySeg{L: l, W: max(pct(hourOf(end))-l, 0.5), Run: run})
	}
	return from, to, segs, pct(hourOf(now))
}

// dayTicks labels the axis every dayBarTick hours from its start.
func dayTicks(from, to int) []DayTick {
	var out []DayTick
	for h := from; h <= to; h += dayBarTick {
		out = append(out, DayTick{Hour: h, Pos: float64(h-from) / float64(to-from) * pctFull})
	}
	return out
}

func init() {
	Register(WidgetType{Key: "kimai_timer", Decode: decodeKimaiLite, Template: "widgets/kimai_timer", Category: CategoryInsight,
		Extra:   ExtraKimaiFavs,
		Service: enums.ServiceKimai, RefreshS: 60, Live: true, View: timerView,
		Queries: func(any) []Query { return []Query{{Name: "live", Source: "kimai.live", Conn: ConnWidget}} }})
}
