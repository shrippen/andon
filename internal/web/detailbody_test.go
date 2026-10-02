package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/services/widgetlib"
	"andon/internal/widgets"
)

// TestDetailBodyBlocks: the generic template draws every block kind,
// typed values in the reader's locale, and tabs as panels.
func TestDetailBodyBlocks(t *testing.T) {
	day := time.Date(2026, 9, 16, 0, 0, 0, 0, time.UTC)
	graph := widgets.LineGraph(widgets.Series{Values: []float64{1, 2, widgets.Gap, 4}, Class: "s1", Label: "CPU"})
	graph.Goal, graph.HasGoal = 3, true
	body := &widgets.DetailBody{
		Line:  []widgets.Fact{{Label: widgets.T("detail.facts"), Value: "nas"}},
		Side:  []widgets.Fact{{Label: widgets.T("detail.facts"), Value: widgets.Money(1234.5, "EUR"), State: "bad"}},
		Facts: []widgets.Kpi{{Value: widgets.Num(3.26, 1), Label: widgets.T("detail.facts"), Tier: "yellow"}},
		Blocks: []widgets.Block{
			{Kind: widgets.BlockGraph, Label: widgets.T("detail.facts"), Hero: true, Data: graph},
			{Kind: widgets.BlockTable, Data: widgets.Table{Head: []widgets.Text{widgets.T("detail.facts")}, Rows: [][]widgets.Cell{{{Value: "sdc", State: "bad"}}}, Num: []int{0}}},
			{Kind: widgets.BlockStrips, Ticks: []any{"a", "b"}, Data: []widgets.Strip{{Name: "borg", States: []string{"ok", "bad"}, Value: "5 h"}}},
			{Kind: widgets.BlockPair, Data: []widgets.Block{
				{Kind: widgets.BlockBars, Data: []widgets.ShareBar{{Name: "tank", Pct: 71, Value: "5,7 TB", Tier: "yellow"}}},
				{Kind: widgets.BlockTimeline, Data: []widgets.Event{{At: day, Title: "update", Sub: "1 → 2", State: widgets.Txt("status.up"), Tier: "cyan"}}},
			}},
			{Kind: widgets.BlockRows, Data: []widgets.LitRow{{Name: "x", Meta: "1", State: "warn"}}},
			{Kind: widgets.BlockStatus, Data: []widgets.LitRow{{Name: "kimai", Meta: "läuft", State: "ok"}}},
			{Kind: widgets.BlockText, Data: "text"},
			{Kind: widgets.BlockCode, Data: "log line"},
			{Kind: widgets.BlockHints, Data: []widgets.DetailHint{{Rule: "scrutiny.disk_hot", Severity: enums.SeverityCritical, Title: "hot", FirstSeen: day}}},
			{Kind: widgets.BlockWall, Data: []widgets.Card{{Label: widgets.T("detail.facts"), Value: "23 %", Spark: []float64{1, 3, 2}}}},
			{Kind: widgets.BlockTasks, Data: widgets.Tasks{Done: 1, Total: 2, Label: widgets.T("detail.facts"), Items: []widgets.Task{{Text: "do", State: "warn", Action: widgets.T("detail.open"), Href: "https://x.example"}}}},
			{Kind: widgets.BlockHeat, Data: widgets.Heat{Rows: 7, Levels: []int{0, 4}}},
			{Kind: widgets.BlockDay, Data: widgets.DayCard{Title: "Mi", State: "ok", StateText: widgets.T("status.up"), Kpis: []widgets.Kpi{{Value: 1, Label: widgets.T("detail.facts")}}}},
			{Kind: widgets.BlockChips, Data: []string{"#a"}},
			{Kind: widgets.BlockWeek, Data: widgets.Week{Start: 6, Span: 16, Days: []widgets.WeekDay{{Label: "Mo", Spans: []widgets.HourSpan{{From: 8.25, To: 12, Colour: "#fe8019"}}, Sum: "3:45", Today: true, Now: 14.5}}}},
			{Kind: widgets.BlockImage, Data: widgets.Image{DataURI: "data:image/png;base64,AAAA", Alt: "x"}},
			{Kind: widgets.BlockRead, Data: widgets.Reading{Title: "Titel", Text: []string{"Absatz"}, Link: "https://x.example/a"}},
			{Kind: widgets.BlockFrame, Data: widgets.Embed{URL: "https://x.example/f"}},
			{Kind: widgets.BlockDayStrip, Data: widgets.DayStrip{Spans: []widgets.HourSpan{{From: 6, To: 12, Colour: "d1"}}, Now: 12}},
		},
		Tabs: []widgets.Tab{{Label: widgets.T("detail.facts"), Count: 3}, {Label: widgets.T("detail.open"), Blocks: []widgets.Block{{Kind: widgets.BlockText, Data: "second"}}}},
	}
	dialog := &widgetlib.DetailDialog{Type: "disks", Head: widgetlib.DetailHead{Title: "Platten", State: "warn", StateKey: "status.up"}, Body: body}

	rec := httptest.NewRecorder()
	if err := (Deps{}).Page(rec, Ctx{Locale: enums.LocaleDE}, detailBlocks, http.StatusOK, map[string]any{"Dialog": dialog, "D": body, "PlacementID": int64(3), "ThemeURL": ""}); err != nil {
		t.Fatal(err)
	}
	got := rec.Body.String()
	for _, want := range []string{
		`class="detail-line"`, `class="detail-side"`, `1.234,50 €`, `<b>3,3</b>`, `class="detail-block detail-hero"`, `class="goal"`,
		`<td class="num" data-state="bad">sdc</td>`, `<path data-state="bad" d="M1.1 0h.8v1h-.8z"/>`, `data-style="--p:71%"`,
		`class="date-tile" datetime="2026-09-16"`, `class="status" data-state="ok"`, `<pre class="codeblock">log line</pre>`, `<iframe src="https://x.example/f"`,
		`class="tier-card" data-tier="red"`, `class="spark"`, `1 / 2`, `class="heat is-weeks"`, `class="sheet detail-day"`,
		`class="chip">#a`, `src="data:image/png;base64,AAAA"`, `<p>Absatz</p>`, `data-style="--from:8.25;--to:12;--c:#fe8019"`, `data-style="--at:14.5"`, `data-style="left:25.0%;width:25.0%;--c:var(--d1)"`, `<span>06</span><span>10</span>`, `data-style="--c:var(--warn)"`, `data-style="--c:var(--d1)"`, `data-detail-tabs`, `data-detail-panel hidden`, `second`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
}

// TestDetailListItems: a list row with an Item opens that entry in the
// dialog; one without stays a plain row.
func TestDetailListItems(t *testing.T) {
	body := &widgets.DetailBody{List: &widgets.ObjList{Label: widgets.T("detail.facts"), Sel: 0, Title: "nas",
		Items: []widgets.LitRow{{Name: "nas", State: "ok", Item: "nas lan"}, {Name: "shop", State: "warn"}}}}
	dialog := &widgetlib.DetailDialog{Type: "monitors", Body: body}
	rec := httptest.NewRecorder()
	if err := (Deps{}).Page(rec, Ctx{Locale: enums.LocaleDE}, detailBlocks, http.StatusOK, map[string]any{"Dialog": dialog, "D": body, "PlacementID": int64(3), "ThemeURL": ""}); err != nil {
		t.Fatal(err)
	}
	got := rec.Body.String()
	for _, want := range []string{`<button type="button" class="list-row" data-details="/details/3?item=nas&#43;lan" aria-selected="true">`, `<div class="list-row">`} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}

// TestDetailTableCSV: tables in pairs and tabs count in drawing order; the
// CSV holds head, rows and foot as the reader sees them.
func TestDetailTableCSV(t *testing.T) {
	first := widgets.Table{Head: []widgets.Text{widgets.T("detail.csv")}, Rows: [][]widgets.Cell{{{Value: widgets.Money(1234.5, "EUR")}}}, Foot: []widgets.Cell{{Value: "Σ"}}}
	body := &widgets.DetailBody{
		Blocks: []widgets.Block{{Kind: widgets.BlockPair, Data: []widgets.Block{{Kind: widgets.BlockTable, Data: first}}}},
		Tabs:   []widgets.Tab{{Blocks: []widgets.Block{{Kind: widgets.BlockTable, Data: widgets.Table{}}}}},
	}
	if n := len(tablesOf(body)); n != 2 {
		t.Fatalf("tables: %d", n)
	}
	blob, err := tableCSV(first, enums.LocaleDE)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(blob); got != "CSV\n\"1.234,50 €\"\nΣ\n" {
		t.Fatalf("csv: %q", got)
	}
}
