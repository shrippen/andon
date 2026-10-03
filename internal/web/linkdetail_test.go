package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/services/linkstatus"
	"andon/internal/services/widgetlib"
	"andon/internal/sources"
)

// TestStackPaths: columns scale to the busiest day, failed checks sit on
// top, a day without checks gets a low mark.
func TestStackPaths(t *testing.T) {
	days := []linkstatus.Day{
		{State: linkstatus.BarNone},
		{State: linkstatus.BarUp, OK: 4},
		{State: linkstatus.BarPartial, OK: 3, Fail: 1},
	}
	got := stackPaths(days)
	want := stackPath{
		OK:   "M1.15 0.0h.7v100.0h-.7zM2.15 25.0h.7v75.0h-.7z",
		Fail: "M2.15 0.0h.7v25.0h-.7z",
		None: "M0.15 96.0h.7v4.0h-.7z",
	}
	if got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

// TestMsChartLeavesGaps: a day without an answer draws no point; the
// slow line stays in view below the top.
func TestMsChartLeavesGaps(t *testing.T) {
	m := msChartOf([]linkstatus.Day{{AvgMs: 100}, {}, {AvgMs: 200}})
	if strings.Count(m.Line, "M") != 1 || strings.Count(m.Line, "L") != 1 {
		t.Fatalf("line %q", m.Line)
	}
	if m.GoalY == "" || strings.HasPrefix(m.GoalY, "-") {
		t.Fatalf("goal %q", m.GoalY)
	}
}

// TestLinkDetailRenders: the dialog shows facts, one day button per day
// with its numbers, and the incidents.
func TestLinkDetailRenders(t *testing.T) {
	today := time.Now().UTC().Truncate(24 * time.Hour)
	days := make([]linkstatus.Day, 3)
	for i := range days {
		days[i] = linkstatus.Day{Day: today.AddDate(0, 0, i-2), State: linkstatus.BarUp, OK: 144, AvgMs: 180}
	}
	days[1] = linkstatus.Day{Day: today.AddDate(0, 0, -1), State: linkstatus.BarDown, Fail: 144, Error: "HTTP 502"}
	detail := &widgetlib.LinkDetail{
		Title: "Nextcloud", URL: "https://cloud.example.test", Accept: []int{401}, Interval: 10 * time.Minute,
		Status: &sources.HTTPStatusResult{Up: true, Code: 200, Ms: 142}, CheckedAt: time.Now(),
		History: linkstatus.History{Days: days, Share30: .66, AvgMs30: 180, Checks: 432,
			Incidents: []linkstatus.Incident{{From: days[1].Day, To: days[1].Day, Days: 1, Failed: 144, Error: "HTTP 502", Down: true}}},
		Info: &sources.LinkInfo{IPs: []string{"203.0.113.24"}, Hops: []sources.LinkHop{{Code: 302, Target: "/login"}, {Code: 200}},
			TLSIssuer: "R11", TLSUntil: time.Now().AddDate(0, 0, 23)},
	}

	dialog := &widgetlib.DetailDialog{Type: "link", Body: detail, Head: widgetlib.DetailHead{Title: "Nextcloud", State: "ok", StateKey: "status.up",
		Actions: []widgetlib.DetailAction{{LabelKey: "linkdetail.check_now", Refresh: true}, {LabelKey: "linkdetail.open", Href: detail.URL, Primary: true}}}}

	rec := httptest.NewRecorder()
	if err := (Deps{}).Page(rec, Ctx{Locale: enums.LocaleDE}, "details/link", http.StatusOK, map[string]any{"Dialog": dialog, "D": detail, "PlacementID": int64(7), "ThemeURL": ""}); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`id="detail-title">Nextcloud`, `data-state="ok"`, `<code>203.0.113.24</code>`, `302 /login → 200`, `2xx, 3xx, 401`,
		`data-v-fail="144"`, `data-v-error="HTTP 502"`, `aria-pressed="true"`, `data-detail-refresh="/widget-fragments/7?refresh"`,
		`href="https://cloud.example.test"`, `Ausfall an einem Tag`, `Zertifikat läuft in`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("missing %q", want)
		}
	}
	if n := strings.Count(body, `<button type="button" aria-pressed=`); n != len(days) {
		t.Errorf("day buttons: %d", n)
	}
}
