package web

import (
	"andon/internal/widgets"
	"bytes"
	"embed"
	"errors"
	"html/template"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"andon/internal/crypto"
	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/services/access"
	"andon/internal/services/onboarding"
	"andon/internal/services/themes"
)

//go:embed templates/*.html
var templateFiles embed.FS

const pctScale = 100

var templates = mustParse()

func mustParse() *template.Template {
	funcs := template.FuncMap{
		// t/money/etc. are bound per-render in Page() via t.Funcs, since
		// they close over the request's locale. These placeholders let the
		// templates parse before that binding happens.
		"t":         func(string, ...any) string { return "" },
		"money":     func(float64, ...string) string { return "" },
		"num":       func(float64, ...int) string { return "" },
		"gb":        func(float64) string { return "" },
		"day":       func(any) string { return "" },
		"weekday":   func(any) string { return "" },
		"pct":       func(float64) string { return "" },
		"ago":       func(any) string { return "" },
		"clockDate": func(string) string { return "" },
		"tt":        func(string, map[string]any) string { return "" },
		"here":      func(string) bool { return false },
		"fragment":  func(*tileBody) (template.HTML, error) { return "", nil },

		// barPct/tier are locale-independent (plain numbers/CSS keywords),
		// so unlike the above they're the real implementation, not a
		// placeholder.
		"barPct":      barPct,
		"abs":         math.Abs,
		"thousands":   func(v float64) float64 { return v / 1000 },
		"sparkOf":     widgets.SparkOf,
		"tier":        tier,
		"eqID":        func(a *int64, b int64) bool { return a != nil && *a == b },
		"weatherKind": weatherKind,
		"clockNow":    func(tz string) string { return clockNow(tz, clockMinutes) },
		"clockNowSec": func(tz string) string { return clockNow(tz, clockSeconds) },
		// clockShow is a clock tile's time: 12 or 24 hours, with or without seconds.
		"clockShow": func(tz string, seconds, h12 bool) string {
			layout := map[[2]bool]string{{false, false}: clockMinutes, {true, false}: clockSeconds,
				{false, true}: clock12Minutes, {true, true}: clock12Seconds}[[2]bool{seconds, h12}]
			return clockNow(tz, layout)
		},
		"clockHands":  clockHands,
		"dict":        dict,
		"list":        func(items ...string) []string { return items },
		"join":        strings.Join,
		"monogram":    monogram,
		"deref":       func(p *enums.TeamRole) enums.TeamRole { return *p },
		"dataURI":     dataURI,
		"mainRuns":    mainRuns,
		"credShape":   func(s enums.ServiceType) string { return string(credShapeOf(s)) },
		"asset":       asset,
		"defaultURL":  defaultURL,
		"canSignIn":   canSignIn,
		"setupFields": setupFieldsOf,
		"projectURL":  projectURL,
		"optText":     optText,
		"secretLabel": secretLabel,
		"alsoLinks":   alsoLinksOf,
	}
	return template.Must(template.New("root").Funcs(funcs).ParseFS(templateFiles, "templates/*.html"))
}

// dataURI marks an inlined image (from the image source) as a safe URL;
// anything else becomes empty.
func dataURI(s string) template.URL {
	if !strings.HasPrefix(s, "data:image/") {
		return ""
	}
	return template.URL(s) //nolint:gosec // built server-side from an image/* response
}

// barPct turns a ratio (e.g. 0.45, or 1.2 over budget) into a 0-100 percent
// for a progress bar's width.
func barPct(ratio float64) int {
	switch {
	case ratio < 0:
		return 0
	case ratio > 1:
		return 100
	default:
		return int(ratio*100 + 0.5)
	}
}

// weatherThresholds maps a WMO weather code's upper bound to its icon key
// (e.g. code 61 -> "rain"): the first threshold the code doesn't exceed.
var weatherThresholds = []struct {
	max  int
	kind string
}{
	{0, "clear"}, {3, "cloudy"}, {48, "fog"}, {57, "drizzle"}, {67, "rain"},
	{77, "snow"}, {82, "showers"}, {86, "snow"}, {99, "thunder"},
}

func weatherKind(code int) string {
	for _, t := range weatherThresholds {
		if code <= t.max {
			return t.kind
		}
	}
	return "unknown"
}

// tier is a progress bar's colour band: red at/over budget, yellow near it.
func tier(ratio float64) string {
	switch {
	case ratio >= 1:
		return "red"
	case ratio >= 0.8:
		return "yellow"
	default:
		return "green"
	}
}

// clockNow formats the current time in an IANA timezone ("" or unknown ->
// server-local). Locale-independent (24h HH:MM[:SS]), unlike clockDate.
func clockNow(tz, layout string) string {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.Local
	}
	return time.Now().In(loc).Format(layout)
}

// Clock layouts: with or without seconds.
const (
	clockMinutes   = "15:04"
	clockSeconds   = "15:04:05"
	clock12Minutes = "3:04 PM"
	clock12Seconds = "3:04:05 PM"
)

// Hands is an analogue clock's hand angles in degrees.
type Hands struct{ H, M, S float64 }

// clockHands is now in tz as hand angles; andon.js moves them on.
func clockHands(tz string) Hands {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.Local
	}
	now := time.Now().In(loc)
	m := float64(now.Minute()) + float64(now.Second())/60
	return Hands{H: float64(now.Hour()%12)*30 + m/2, M: m * 6, S: float64(now.Second()) * 6}
}

func clockDate(tz string, locale enums.Locale) string {
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.Local
	}
	now := time.Now().In(loc)
	return i18n.Weekday(now, locale) + ", " + i18n.Day(now, locale)
}

// renderState is what a pooled template set's funcs read during one
// render: the locale, the request path, money rounding and the page data.
type renderState struct {
	locale enums.Locale
	path   string
	round  widgets.RoundMode
	data   map[string]any
	tile   map[string]any // data plus one tile's fragment, reused per tile
}

// pageSet is a clone of the templates whose funcs read state. Reused:
// cloning the whole set per request cost ~3.7 MB and dominated each of a
// board's tile fragments (50 link tiles → 50 clones per page view).
//
//	pageSets ─► pageSet{tmpl, state} ─► set state ─► execute ─► reset ─► pageSets
type pageSet struct {
	tmpl  *template.Template
	state *renderState
}

// maxPageSets bounds the idle sets (~3 MB each). A channel, not a
// sync.Pool: every GC empties a pool, and rendering allocates enough to
// run GC often.
const maxPageSets = 8

var pageSets = make(chan *pageSet, maxPageSets)

// takeSet returns an idle set, or a new one when all are busy.
func takeSet() *pageSet {
	select {
	case set := <-pageSets:
		return set
	default:
		return newPageSet()
	}
}

// returnSet clears the set's state and keeps it unless enough are idle.
func returnSet(set *pageSet) {
	*set.state = renderState{}
	select {
	case pageSets <- set:
	default:
	}
}

// newPageSet clones the templates once and binds the per-render helpers
// to the set's own state.
func newPageSet() *pageSet {
	st := &renderState{}
	set := &pageSet{tmpl: template.Must(templates.Clone()), state: st}
	set.tmpl.Funcs(template.FuncMap{
		"t": func(key string, kv ...any) string { return i18n.T(key, st.locale, pairs(kv)) },
		"money": func(v float64, currency ...string) string {
			return moneyFunc(st.locale, st.round)(v, currency...)
		},
		"num":       func(v float64, digits ...int) string { return i18n.Num(v, st.locale, firstOr(digits, 0)) },
		"gb":        func(v float64) string { return i18n.GB(v, st.locale) },
		"day":       func(v any) string { return i18n.Day(v, st.locale) },
		"weekday":   func(v any) string { return i18n.Weekday(v, st.locale) },
		"pct":       func(v float64) string { return i18n.Num(v*pctScale, st.locale, 0) + " %" },
		"ago":       func(v any) string { return i18n.Ago(asTimePtr(v), st.locale) },
		"clockDate": func(tz string) string { return clockDate(tz, st.locale) },
		// here tells whether the page lies at or below path (menu underline).
		"here": func(path string) bool { return underPath(st.path, path) },
		// tt translates with typed params ({"$money": 12.5} -> "12,50 €").
		"tt": func(key string, params map[string]any) string {
			return i18n.T(key, st.locale, i18n.Typed(params, st.locale))
		},
		"fragment": set.fragment,
	})
	return set
}

// fragment renders a tile body inside the page, with the page's data
// plus the fragment, as /widget-fragments/{id} would answer.
func (s *pageSet) fragment(body *tileBody) (template.HTML, error) {
	// One copy of the page data per render, not per tile: tile bodies
	// render one after another.
	own := s.state.tile
	if own == nil {
		own = make(map[string]any, len(s.state.data)+2)
		for k, v := range s.state.data {
			own[k] = v
		}
		s.state.tile = own
	}
	own["Frag"], own["PlacementID"] = body.Frag, body.PlacementID

	// A tile that rounds money formats with its own rounding.
	pageRound := s.state.round
	defer func() { s.state.round = pageRound }()
	if body.Frag != nil && body.Frag.Frame.Round != widgets.RoundExact && body.Frag.Frame.Round != "" {
		s.state.round = body.Frag.Frame.Round
	}

	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, body.Template, own); err != nil {
		return "", err
	}
	if body.Frag != nil && body.Frag.Calm() {
		buf.WriteString(calmMark)
	}
	return template.HTML(buf.String()), nil //nolint:gosec // output of our own escaping templates
}

// Page renders a full page with the common translation/formatting helpers
// bound to ctx.Locale, and the active theme's stylesheet (unless
// the caller already set "ThemeURL" itself — the board page picks its own
// board/space-scoped theme).
func (d Deps) Page(w http.ResponseWriter, ctx Ctx, name string, status int, values map[string]any) error {
	// A fresh mask per answer: compressed pages must not repeat the
	// token (BREACH).
	shown := ctx
	if shown.CSRF != "" {
		shown.CSRF = crypto.MaskToken(ctx.CSRF)
	}
	data := map[string]any{"Ctx": shown, "Who": ctx.Who, "CSRFField": CSRFField, "CSRFHeader": CSRFHeader}
	for k, v := range values {
		data[k] = v
	}
	// A tile fragment or a board section has no header: skip the nav and
	// onboarding queries that every one of them would otherwise repeat.
	_, fragment := data["Frag"]
	_, partial := data["Partial"]
	if ctx.Who != nil && !fragment && !partial {
		d.addNav(data, ctx.Who)
		// Menu progress and page intros (see routes_welcome.go).
		if _, ok := data["Onboarding"]; !ok {
			state, err := onboarding.Load(d.DB, ctx.Who)
			if err != nil {
				state = onboarding.State{} // templates read it; no intros, no progress
			}
			data["Onboarding"] = state
		}
	}
	if _, ok := data["ThemeURL"]; !ok {
		url, err := d.themeURL(ctx.Who, nil, nil)
		if err != nil {
			return err
		}
		data["ThemeURL"] = url
	}

	set := takeSet()
	defer returnSet(set)
	*set.state = renderState{locale: ctx.Locale, path: ctx.Path, round: roundOf(values["Round"]), data: data}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	// Logged here: most callers ignore the error once headers are out, and
	// a broken template would otherwise leave half a page and no trace.
	if err := set.tmpl.ExecuteTemplate(w, name, data); err != nil {
		slog.Warn("render", "template", name, "err", err)
		return err
	}
	return nil
}

// calmMark ends a tile body that has nothing to do and asks to be hidden
// (widgets.Frame.OnlyIssues); CSS hides the tile around it.
const calmMark = `<i class="calm-mark" hidden></i>`

// moneyFunc formats money in the locale, rounded as a tile's frame asks.
func moneyFunc(locale enums.Locale, round widgets.RoundMode) func(float64, ...string) string {
	return func(v float64, currency ...string) string {
		c := i18n.DefaultCurrency
		if len(currency) > 0 && currency[0] != "" {
			c = currency[0]
		}
		return i18n.MoneyRound(v, locale, c, string(round))
	}
}

// roundOf reads a page's "Round" value (a fragment's frame).
func roundOf(v any) widgets.RoundMode {
	r, _ := v.(widgets.RoundMode)
	return r
}

// themeURL resolves the CSS URL of the theme active for who (nil for an
// anonymous page — login, setup — which gets the instance default),
// optionally narrowed to a board's or space's own theme choice.
func (d Deps) themeURL(who *access.Principal, boardTheme, spaceID *int64) (string, error) {
	themeID, err := themes.Active(d.DB, who, boardTheme, spaceID)
	if err != nil {
		return "", err
	}
	_, version, err := themes.Stylesheet(d.DB, themeID)
	if err != nil {
		return "", err
	}
	return "/theme/" + strconv.FormatInt(themeID, 10) + ".css?v=" + strconv.Itoa(version), nil
}

// dict packs key/value pairs into a map, so a sub-template invoked with
// {{template "name" dict "A" .X "B" $}} can take more than the single
// pipeline argument {{template}} otherwise allows.
func dict(kv ...any) (map[string]any, error) {
	if len(kv)%2 != 0 {
		return nil, errors.New("dict: odd number of arguments")
	}
	out := make(map[string]any, len(kv)/2)
	for i := 0; i < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			return nil, errors.New("dict: keys must be strings")
		}
		out[key] = kv[i+1]
	}
	return out, nil
}

func firstOr[T any](vals []T, def T) T {
	if len(vals) > 0 {
		return vals[0]
	}
	return def
}

// underPath reports whether cur is path or a page below it
// (/connections/new belongs to /connections, /connectionsx does not).
func underPath(cur, path string) bool {
	return cur == path || strings.HasPrefix(cur, path+"/")
}

// asTimePtr turns a slot's OkAt (a zero time.Time when unset) into the
// pointer i18n.Ago expects.
func asTimePtr(v any) *time.Time {
	if p, ok := v.(*time.Time); ok && p != nil {
		v = *p
	}
	t, ok := v.(time.Time)
	if !ok || t.IsZero() {
		return nil
	}
	return &t
}

// pairs turns a flat ["key", value, "key2", value2, ...] slice into a map,
// so templates can write {{t "hint.x" "hours" 3}}.
func pairs(kv []any) map[string]any {
	out := map[string]any{}
	for i := 0; i+1 < len(kv); i += 2 {
		if key, ok := kv[i].(string); ok {
			out[key] = kv[i+1]
		}
	}
	return out
}

// monogram is the fallback icon text: initials of two words, else the
// first two letters ("Invoice Ninja" -> "IN", "Kimai" -> "KI").
func monogram(title string) string {
	words := strings.Fields(strings.ReplaceAll(title, "-", " "))
	var letters []rune
	if len(words) > 1 {
		for _, w := range words[:2] {
			letters = append(letters, []rune(w)[0])
		}
	} else {
		letters = []rune(title)
		if len(letters) > 2 {
			letters = letters[:2]
		}
	}
	if len(letters) == 0 {
		return "?"
	}
	return strings.ToUpper(string(letters))
}
