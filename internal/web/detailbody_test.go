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
		`class="date-tile" datetime="2026-09-16"`, `class="status" data-state="ok"`, `<pre class="codeblock">log line</pre>`,
		`class="tier-card" data-tier="red"`, `class="spark"`, `1 / 2`, `class="heat is-weeks"`, `class="sheet detail-day"`,
		`class="chip">#a`, `data-style="--c:var(--warn)"`, `data-style="--c:var(--d1)"`, `data-detail-tabs`, `data-detail-panel hidden`, `second`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q", want)
		}
	}
}
