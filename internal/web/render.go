package web

import (
	"bytes"
	"cmp"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"

	"andon/internal/widgets"

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
		"t":            func(string, ...any) string { return "" },
		"money":        func(float64, ...string) string { return "" },
		"num":          func(float64, ...int) string { return "" },
		"serviceNames": func([]string) string { return "" },
		"nums":         func([]float64, int) string { return "" },
		"gb":           func(float64) string { return "" },
		"day":          func(any) string { return "" },
		"weekday":      func(any) string { return "" },
		"mday":         func(any) string { return "" },
		"month":        func(any) string { return "" },
		"pct":          func(float64) string { return "" },
		"ago":          func(any) string { return "" },
		"clockDate":    func(string) string { return "" },
		"tt":           func(string, map[string]any) string { return "" },
		"tv":           func(any) string { return "" },
		"tvs":          func([]any) string { return "" },
		"here":         func(string) bool { return false },
		"at":           func(string) bool { return false },
		"fragment":     func(*tileBody) (template.HTML, error) { return "", nil },

		// known is the first of keys the catalog has, else "": picks a
		// tile type's own label over the shared one.
		"known": func(keys ...string) string {
			for _, k := range keys {
				if i18n.Has(k) {
					return k
				}
			}
			return ""
		},

		// barPct/tier are locale-independent (plain numbers/CSS keywords),
		// so unlike the above they're the real implementation, not a
		// placeholder.
		"barPct":      barPct,
		"every":       every,
		"uptimePaths": uptimePaths,
		"stackPaths":  stackPaths,
		"chartGeom":   geomOf,
		"sevTier":     sevTier,
		"stateVar":    stateVar,
		"seriesVar":   seriesVar,
		"numCol":      numCol,
		"graphLegend": graphLegend,
		"graphUnit":   graphUnit,
		"stripStates": stripStates,
		"stateFill":   stateFill,
		"weekKeys":    weekKeys,
		"pctOf":       pctOf,
		"weekScale":   weekScale,
		"hourPct":     hourPct,
		"spanLen":     spanLen,
		"isHex":       isHex,
		"stripPaths":  stripPaths,
		"sparkPath":   sparkPath,
		"trendPath":   trendPath,
		"msChartOf":   msChartOf,
		"msX":         msX,
		"kanteState":  kanteState,
		"percent":     func(v float64) float64 { return v * pctScale },
		"seconds":     func(v time.Duration) float64 { return v.Seconds() },
		"minutes":     func(v time.Duration) float64 { return v.Minutes() },
		"lastIndex":   func(n int) int { return n - 1 },
		"launchEvery": func() string { return every(launchRefreshS) },
		"abs":         math.Abs,
		"thousands":   func(v float64) float64 { return v / 1000 },
		"sparkOf":     widgets.SparkOf,
		"tier":        tier,
		"pill":        pillState,
		"eqID":        func(a *int64, b int64) bool { return a != nil && *a == b },
		"weatherKind": widgets.WeatherKind,
		"json":        toJSON,
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

// toJSON writes a value as JSON for a data attribute (a map's route).
func toJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	return string(b), err
}

// pillStates maps the states services report (ok, warn, fail) to the
// states of Kante's .pill.
var pillStates = map[string]string{"ok": "applied", "warn": "locked", "fail": "failed"}

// pillState is the data-state of a .pill for a service state; states that
// are already Kante's (reviewing, ...) and "" pass through.
func pillState(state string) string {
	if kante, ok := pillStates[state]; ok {
		return kante
	}
	return state
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
	nav    string // the settings page a page counts as (navPath), else path
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
		"num": func(v float64, digits ...int) string { return i18n.Num(v, st.locale, firstOr(digits, 0)) },
		// Hover values of a chart line, "|"-separated as decimals use commas: 1,5|2,25.
		"nums": func(vs []float64, digits int) string {
			out := make([]string, len(vs))
			for i, v := range vs {
				out[i] = i18n.Num(v, st.locale, digits)
			}
			return strings.Join(out, "|")
		},
		// Service names of a list: "kimai", "sure" → "Kimai, Sure".
		"serviceNames": func(services []string) string {
			out := make([]string, len(services))
			for i, s := range services {
				out[i] = i18n.T("service."+s, st.locale, nil)
			}
			return strings.Join(out, ", ")
		},
		"gb":        func(v float64) string { return i18n.GB(v, st.locale) },
		"day":       func(v any) string { return i18n.Day(v, st.locale) },
		"weekday":   func(v any) string { return i18n.Weekday(v, st.locale) },
		"mday":      i18n.MonthDay,
		"month":     func(v any) string { return i18n.MonthShort(v, st.locale) },
		"pct":       func(v float64) string { return i18n.Num(v*pctScale, st.locale, 0) + " %" },
		"ago":       func(v any) string { return i18n.Ago(asTimePtr(v), st.locale) },
		"clockDate": func(tz string) string { return clockDate(tz, st.locale) },
		// here tells whether the page lies at or below path (menu underline).
		"here": func(path string) bool { return underPath(st.path, path) },
		// at tells whether the page is exactly path (settings navigation).
		"at": func(path string) bool { return cmp.Or(st.nav, st.path) == path },
		// tt translates with typed params ({"$money": 12.5} -> "12,50 €").
		"tt": func(key string, params map[string]any) string {
			return i18n.T(key, st.locale, i18n.Typed(params, st.locale))
		},
		// tv shows a value of a detail dialog: text as is, a typed value
		// ({"$num": 3.5, "digits": 1}) per locale.
		"tv": func(v any) string {
			if v == nil {
				return ""
			}
			return fmt.Sprint(i18n.Typed(map[string]any{"v": v}, st.locale)["v"])
		},
		// tvs joins values as tv shows them, "|"-separated like nums: the
		// x labels of a chart line's hover.
		"tvs": func(vs []any) string {
			out := make([]string, len(vs))
			for i, v := range vs {
				out[i] = fmt.Sprint(i18n.Typed(map[string]any{"v": v}, st.locale)["v"])
			}
			return strings.Join(out, "|")
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
		groups := settingsNav(ctx.Who)
		data["SetupHref"] = setupHref(groups)
		if settingsPages[name] {
			nav, _ := values[navPath].(string)
			data["SettingsNav"], data["SettingsSetup"] = navScope(groups, cmp.Or(nav, ctx.Path))
		}
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
	nav, _ := values[navPath].(string)
	*set.state = renderState{locale: ctx.Locale, path: ctx.Path, nav: nav, round: roundOf(values["Round"]), data: data}

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

// refreshShare: a tile polls every seconds ± seconds/refreshShare/2;
// launchRefreshS is a link tile's poll.
const (
	refreshShare   = 10
	launchRefreshS = 300
)

// every is a tile's htmx poll trigger with a period picked at random
// per render, so a board's tiles drift apart instead of reaching the
// server, and through it the services, all in the same second:
//
//	300 → "every 286s" … "every 314s"
func every(seconds int) string {
	if spread := seconds / refreshShare; spread > 0 {
		seconds += rand.IntN(spread) - spread/2
	}
	return "every " + strconv.Itoa(seconds) + "s"
}
