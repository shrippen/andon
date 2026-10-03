package web

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"andon/internal/enums"
	"andon/internal/services/linkstatus"
	"andon/internal/services/widgetlib"
)

// TestUptimePaths: one path per state present, one column per day at
// the day's position, in Kante's state names.
func TestUptimePaths(t *testing.T) {
	u := linkstatus.Uptime{Bars: []linkstatus.Bar{
		{State: linkstatus.BarNone}, {State: linkstatus.BarUp}, {State: linkstatus.BarDown}, {State: linkstatus.BarUp},
	}}
	want := []uptimePath{
		{State: "off", D: "M0 0h.7v1h-.7z"},
		{State: "ok", D: "M1 0h.7v1h-.7zM3 0h.7v1h-.7z"},
		{State: "bad", D: "M2 0h.7v1h-.7z"},
	}
	if got := uptimePaths(u); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// TestLinkUptimeRenders: a link tile draws its strip as one SVG with
// the summary as its accessible name.
func TestLinkUptimeRenders(t *testing.T) {
	up := linkstatus.Uptime{Share: 1, AvgMs: 120, Bars: []linkstatus.Bar{{State: linkstatus.BarUp}, {State: linkstatus.BarUp}}}
	frag := &widgetlib.Fragment{Type: "link", View: map[string]any{"Uptime": up}}

	rec := httptest.NewRecorder()
	if err := (Deps{}).Page(rec, Ctx{Locale: enums.LocaleDE}, "widgets/link", http.StatusOK, map[string]any{"ThemeURL": "", "Frag": frag}); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	for _, want := range []string{`<svg class="uptime" viewBox="0 0 2 1"`, `aria-label="100`, `<path data-state="ok" d="M0 0h.7v1h-.7zM1 0h.7v1h-.7z"/>`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q in %s", want, body)
		}
	}
	if strings.Contains(body, "<i ") {
		t.Fatalf("expected no per-day elements: %s", body)
	}
}
