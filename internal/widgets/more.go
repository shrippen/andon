package widgets

// Start widgets without an own service (Dashy's widget list): calendar,
// custom JSON API, static list, holidays, xkcd, NASA picture, jokes,
// crypto, stocks, flights and public transport.

import (
	"cmp"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

const (
	defaultListLimit = 8
	defaultCalDays   = 14
	defaultCountry   = "DE"
	defaultCurrency  = "eur"
	timeOfDay        = "15:04"
	pathSep          = "."
	fieldSep         = "="
)

// ── configs ──

// CalendarConfig: URL is a private iCal address, stored sealed.
type CalendarConfig struct {
	URL        string
	More       []string // further calendars ("" = unused slot)
	Colors     []string // per calendar: the first, then More
	Days       int
	Limit      int
	HideAllDay bool
}

// calendarSlots is how many calendars one tile merges.
const calendarSlots = 3

func decodeCalendar(r Raw) CalendarConfig {
	cfg := CalendarConfig{URL: r.URL("ical_url"), Days: r.Int("days"), Limit: r.Int("limit"), HideAllDay: r.Bool("hide_all_day")}
	for i := 2; i <= calendarSlots; i++ {
		cfg.More = append(cfg.More, r.URL("ical_url_"+strconv.Itoa(i)))
	}
	for i := 1; i <= calendarSlots; i++ {
		cfg.Colors = append(cfg.Colors, r.Pick("color_"+strconv.Itoa(i)))
	}
	return cfg
}

// calendarQueries asks each configured calendar; the first answers as
// "events", the others as "events2", "events3".
func calendarQueries(cfg CalendarConfig) []Query {
	q := []Query{{Name: "events", Source: "ical", Params: map[string]any{"url": cfg.URL, "days": float64(cfg.Days)}}}
	for i, u := range cfg.More {
		if u != "" {
			q = append(q, Query{Name: "events" + strconv.Itoa(i+2), Source: "ical", Params: map[string]any{"url": u, "days": float64(cfg.Days)}})
		}
	}
	return q
}

// calendarPast reads the last weeks of every calendar ("past", "past2",
// …) and Kimai's bookings, for the dialog's unbooked appointments.
func calendarPast(cfg CalendarConfig) []Query {
	var q []Query
	for i, u := range append([]string{cfg.URL}, cfg.More...) {
		if u == "" {
			continue
		}
		name := pastName
		if i > 0 {
			name += strconv.Itoa(i + 1)
		}
		q = append(q, Query{Name: name, Source: "ical", Params: map[string]any{"url": u, "days": 0.0, "back": float64(metrics.UnbookedDays)}})
	}
	return append(q, kimaiPeer)
}

// pastName names the dialog's look back at a calendar.
const pastName = "past"

// APIField is one value picked from a JSON body: "Temp = main.temp".
type APIField struct {
	Label, Path string
}

type CustomAPIConfig struct {
	URL        string
	Headers    map[string]string
	Fields     []APIField
	Thresholds []Threshold
	Units      map[string]string // by lower-case label
}

func decodeCustomAPI(r Raw) CustomAPIConfig {
	cfg := CustomAPIConfig{URL: r.URL("url"), Headers: stringMap(r.Get("headers")), Thresholds: parseThresholds(r.String("thresholds")),
		Units: parsePairs(r.String("units"))}
	for _, line := range strings.Split(r.String("fields"), "\n") {
		label, path, ok := strings.Cut(line, fieldSep)
		if !ok {
			label, path = line, line
		}
		if path = strings.TrimSpace(path); path == "" {
			continue
		}
		cfg.Fields = append(cfg.Fields, APIField{Label: strings.TrimSpace(label), Path: path})
	}
	return cfg
}

// ListEntry is one line of a static list; URL may be "".
type ListEntry struct {
	Text, URL string
}

type ListConfig struct {
	Entries []ListEntry
	TwoCols bool
}

func decodeList(r Raw) ListConfig {
	cfg := ListConfig{TwoCols: r.Bool("two_columns")}
	for _, line := range strings.Split(r.String("entries"), "\n") {
		text, link, _ := strings.Cut(line, linkSep)
		text, link = strings.TrimSpace(text), strings.TrimSpace(link)
		if text == "" && link == "" {
			continue
		}
		if !strings.HasPrefix(link, "https://") && !strings.HasPrefix(link, "http://") {
			link = ""
		}
		cfg.Entries = append(cfg.Entries, ListEntry{Text: cmp.Or(text, link), URL: link})
	}
	return cfg
}

type HolidaysConfig struct {
	Country, State string
	Limit          int
	Bridges        bool // name the bridge day next to a Tuesday or Thursday holiday
}

// decodeHolidays prefixes a bare state with its country, as Nager names
// regions: "BY" → "DE-BY".
func decodeHolidays(r Raw) HolidaysConfig {
	country := strings.ToUpper(textOr(r, "country"))
	state := strings.ToUpper(strings.TrimSpace(r.String("state")))
	if state != "" && !strings.Contains(state, "-") {
		state = country + "-" + state
	}
	return HolidaysConfig{Country: country, State: state, Limit: r.Int("limit"), Bridges: r.Bool("bridges")}
}

type JokeConfig struct {
	Category, Lang string
	EveryH         int // hours until the next joke, 0 = the default hour
}

// decodeJoke passes any category and language on: JokeAPI knows more
// than the form offers (a Dashy import may bring "fr").
func decodeJoke(r Raw) JokeConfig {
	return JokeConfig{Category: textOr(r, "category"), Lang: textOr(r, "lang"), EveryH: r.Int("every")}
}

// RefreshSeconds shows the next joke at the chosen pace.
func (c JokeConfig) RefreshSeconds() int { return c.EveryH * secondsPerMinute * secondsPerMinute }

// PictureConfig is the xkcd and APOD widgets' config.
type PictureConfig struct {
	APIKey    string
	ImageOnly bool // no title and text
	Random    bool // xkcd: any comic instead of the newest
}

func decodePicture(r Raw) PictureConfig {
	return PictureConfig{APIKey: r.String("api_key"), ImageOnly: r.Bool("image_only"), Random: r.Bool("random")}
}

func pictureView(cfg PictureConfig, _ map[string]any, _ ViewCtx) map[string]any {
	return map[string]any{"ImageOnly": cfg.ImageOnly}
}

type CryptoConfig struct {
	Coins    []string
	Currency string
	Spark    bool // 7-day line
	Digits   int  // decimals of the price
}

// cryptoDigits is the default number of decimals.
const cryptoDigits = 2

// Coins and tickers a new tile lists.
var (
	defaultCoins   = []string{"bitcoin", "ethereum"}
	defaultTickers = []string{"aapl.us", "sap.de"}
)

func decodeCrypto(r Raw) CryptoConfig {
	return CryptoConfig{Coins: listOr(r, "coins"), Currency: strings.ToLower(textOr(r, "currency")), Spark: r.Bool("spark"),
		Digits: r.Int("digits")}
}

type StocksConfig struct {
	Symbols []string
	Week    bool // change over a week instead of since the last close
	Spark   bool // a month's line
}

func decodeStocks(r Raw) StocksConfig {
	return StocksConfig{Symbols: listOr(r, "tickers"), Week: r.Pick("change") == "week", Spark: r.Bool("spark")}
}

type FlightsConfig struct {
	Airport, Direction, APIKey string
	Limit                      int
	Airlines                   []string // flight number prefixes ("LH", "EW"), upper case; empty = all
}

func decodeFlights(r Raw) FlightsConfig {
	return FlightsConfig{Airport: strings.ToUpper(r.String("airport")), Direction: r.Pick("direction"), APIKey: r.String("api_key"),
		Limit: r.Int("limit"), Airlines: upperList(r.List("airlines"))}
}

type TransitConfig struct {
	Stop  string
	Limit int
	Lines []string // line names, upper case; empty = all
	Walk  int      // minutes to the stop; what leaves sooner is left out
}

func decodeTransit(r Raw) TransitConfig {
	return TransitConfig{Stop: r.String("stop"), Limit: r.Int("limit"), Lines: upperList(r.List("lines")), Walk: r.Int("walk")}
}

// textOr is a text field, its Default while empty.
func textOr(r Raw, key string) string {
	return cmp.Or(r.String(key), textOf(r.field(key).Default))
}

// listOr is a list field, its Default while empty: a tile needs
// something to show.
func listOr(r Raw, key string) []string {
	if list := r.List(key); len(list) > 0 {
		return list
	}
	return asStringList(r.field(key).Default)
}

// upperList is a list in upper case without spaces ("S 1" = "S1").
func upperList(list []string) []string {
	var out []string
	for _, s := range list {
		if s = strings.ToUpper(strings.ReplaceAll(s, " ", "")); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// boardFetch is how many movements to ask for when a filter drops some.
func boardFetch(limit int, filtered bool) int {
	if filtered {
		return min(limit*3, 60)
	}
	return limit
}

// ── views ──

// clockZone is where times of day are shown.
func clockZone() *time.Location {
	if loc, err := time.LoadLocation(defaultTimezone); err == nil {
		return loc
	}
	return time.UTC
}

// CalRow is one event as shown.
type CalRow struct {
	Day, Time, Title, Location string
	Color                      string // accent of its calendar, "none" = plain
	at                         time.Time
}

func calendarView(cfg CalendarConfig, results map[string]any, _ ViewCtx) map[string]any {
	zone := clockZone()
	var rows []CalRow
	found := false
	for i := range calendarSlots {
		name := "events"
		if i > 0 {
			name += strconv.Itoa(i + 1)
		}
		data, ok := results[name].(*sources.CalendarResult)
		if !ok {
			continue
		}
		found = true
		color := "none"
		if i < len(cfg.Colors) {
			color = cfg.Colors[i]
		}
		for _, e := range data.Events {
			if e.AllDay && cfg.HideAllDay {
				continue
			}
			at := e.Start.In(zone)
			row := CalRow{Day: at.Format(isoDate), Title: e.Title, Location: e.Location, Color: color, at: e.Start}
			if !e.AllDay {
				row.Time = at.Format(timeOfDay)
			}
			rows = append(rows, row)
		}
	}
	if !found {
		return map[string]any{}
	}
	sort.SliceStable(rows, func(a, b int) bool { return rows[a].at.Before(rows[b].at) })
	return map[string]any{"Rows": firstN(rows, cfg.Limit)}
}

const isoDate = "2006-01-02"

// APIValue is one picked field; Missing when the path matched nothing.
type APIValue struct {
	Label, Value string
	Unit, Level  string
	Missing      bool
}

func customAPIView(cfg CustomAPIConfig, results map[string]any, _ ViewCtx) map[string]any {
	data, ok := results["body"].(*sources.JSONResult)
	if !ok {
		return map[string]any{}
	}
	var rows []APIValue
	for _, f := range cfg.Fields {
		v, found := jsonPath(data.Body, f.Path)
		row := APIValue{Label: f.Label, Value: textOf(v), Missing: !found, Unit: cfg.Units[strings.ToLower(f.Label)]}
		if n, ok := numberOf(v); ok && found {
			row.Level = levelOf(f.Label, n, cfg.Thresholds)
		}
		rows = append(rows, row)
	}
	return map[string]any{"Rows": rows}
}

// jsonPath walks "a.b.0.c" through maps and lists.
func jsonPath(body any, path string) (any, bool) {
	cur := body
	for _, part := range strings.Split(path, pathSep) {
		switch node := cur.(type) {
		case map[string]any:
			next, ok := node[part]
			if !ok {
				return nil, false
			}
			cur = next
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(node) {
				return nil, false
			}
			cur = node[i]
		default:
			return nil, false
		}
	}
	return cur, true
}

// scalarPaths lists the paths of a body's numbers, texts and flags,
// depth first, at most limit: {"main": {"temp": 3}} → ["main.temp"].
func scalarPaths(body any, prefix string, limit int, out []string) []string {
	join := func(k string) string {
		if prefix == "" {
			return k
		}
		return prefix + pathSep + k
	}
	switch node := body.(type) {
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(node)) {
			if len(out) >= limit {
				break
			}
			out = scalarPaths(node[k], join(k), limit, out)
		}
	case []any:
		for i, v := range node {
			if len(out) >= limit {
				break
			}
			out = scalarPaths(v, join(strconv.Itoa(i)), limit, out)
		}
	default:
		if prefix != "" && len(out) < limit {
			out = append(out, prefix)
		}
	}
	return out
}

// AddAPIField appends a field "label = path" to a custom_api tile's
// fields text; the label is the path's last part.
func AddAPIField(fields, path string) string {
	label := path[strings.LastIndex(path, pathSep)+1:]
	line := label + " " + fieldSep + " " + path
	if strings.TrimSpace(fields) == "" {
		return line
	}
	return strings.TrimRight(fields, "\n") + "\n" + line
}

// HolidayRow is one upcoming holiday with the days left.
type HolidayRow struct {
	Day, Name string
	In        int
	Bridge    string // the bridge day, "" if none or not asked
}

func holidaysView(cfg HolidaysConfig, results map[string]any, ctx ViewCtx) map[string]any {
	data, ok := results["days"].(*sources.HolidaysResult)
	if !ok {
		return map[string]any{}
	}
	today := todayOf(ctx)
	var rows []HolidayRow
	for _, h := range data.Days {
		if len(rows) >= cfg.Limit {
			break
		}
		at, err := time.Parse(isoDate, h.Day)
		if err != nil {
			continue
		}
		row := HolidayRow{Day: h.Day, Name: h.Name, In: int(at.Sub(today).Hours() / hoursPerDayInsight)}
		if cfg.Bridges {
			row.Bridge = bridgeDay(at)
		}
		rows = append(rows, row)
	}
	return map[string]any{"Rows": rows}
}

const hoursPerDayInsight = 24

// bridgeDay is the working day that joins a holiday to the weekend:
// Monday before a Tuesday, Friday after a Thursday; "" otherwise.
func bridgeDay(holiday time.Time) string {
	switch holiday.Weekday() {
	case time.Tuesday:
		return holiday.AddDate(0, 0, -1).Format(isoDate)
	case time.Thursday:
		return holiday.AddDate(0, 0, 1).Format(isoDate)
	}
	return ""
}

// MoveRow is one departure/arrival as shown.
type MoveRow struct {
	Time, Line, Place, Status, Platform string
	Delay                               int
	Canceled                            bool
	Remarks                             []string
}

func boardView(limit int, data *sources.BoardResult) map[string]any {
	zone := clockZone()
	var rows []MoveRow
	for _, m := range data.Movements {
		if len(rows) >= limit {
			break
		}
		rows = append(rows, MoveRow{Time: m.When.In(zone).Format(timeOfDay), Line: m.Line, Place: m.Place,
			Status: m.Status, Platform: m.Platform, Delay: m.Delay, Canceled: m.Canceled, Remarks: m.Remarks})
	}
	return map[string]any{"Stop": data.Stop, "Rows": rows}
}

func flightsView(cfg FlightsConfig, results map[string]any, _ ViewCtx) map[string]any {
	data, ok := results["board"].(*sources.BoardResult)
	if !ok {
		return map[string]any{}
	}
	if len(cfg.Airlines) > 0 {
		shown := *data
		shown.Movements = nil
		for _, m := range data.Movements {
			flight := strings.ToUpper(strings.ReplaceAll(m.Line, " ", ""))
			if slices.ContainsFunc(cfg.Airlines, func(a string) bool { return strings.HasPrefix(flight, a) }) {
				shown.Movements = append(shown.Movements, m)
			}
		}
		data = &shown
	}
	return boardView(cfg.Limit, data)
}

func transitView(cfg TransitConfig, results map[string]any, _ ViewCtx) map[string]any {
	data, ok := results["board"].(*sources.BoardResult)
	if !ok {
		return map[string]any{}
	}
	if len(cfg.Lines) > 0 || cfg.Walk > 0 {
		reach := time.Now().Add(time.Duration(cfg.Walk) * time.Minute)
		shown := *data
		shown.Movements = nil
		for _, m := range data.Movements {
			line := strings.ToUpper(strings.ReplaceAll(m.Line, " ", ""))
			if len(cfg.Lines) > 0 && !slices.Contains(cfg.Lines, line) {
				continue
			}
			if cfg.Walk > 0 && m.When.Before(reach) {
				continue
			}
			shown.Movements = append(shown.Movements, m)
		}
		data = &shown
	}
	return boardView(cfg.Limit, data)
}

// ── registration ──

// one builds a single-query Queries func.
func one[C any](name, source string, params func(cfg C) map[string]any) func(C) []Query {
	return func(cfg C) []Query {
		return []Query{{Name: name, Source: source, Params: params(cfg)}}
	}
}

func init() {
	const (
		minute = 60
		hour   = 60 * minute
	)

	Tile[CalendarConfig]{Key: "calendar", Detail: calendarDetail, Category: CategoryStart, Topic: TopicOverview, RefreshS: 15 * minute,
		Fields: []Field{{Key: "ical_url", Input: InputSecret}, sel("color_1", "none", accentColors...),
			{Key: "ical_url_2", Input: InputSecret}, sel("color_2", "none", accentColors...),
			{Key: "ical_url_3", Input: InputSecret}, sel("color_3", "none", accentColors...),
			{Key: "days", Input: InputNumber, Default: defaultCalDays, Min: "1", Max: "90"},
			{Key: "limit", Input: InputNumber, Default: defaultListLimit, Min: "1", Max: "50"}, {Key: "hide_all_day", Input: InputCheck}},
		Decode: decodeCalendar, Queries: calendarQueries, DetailQueries: calendarPast, View: calendarView}.add()

	Tile[CustomAPIConfig]{Key: "custom_api", Detail: customAPIDetail, Category: CategoryStart, Topic: TopicAnalysis, RefreshS: 5 * minute,
		Fields: []Field{{Key: "url", Input: InputText, Required: true}, {Key: "headers", Input: InputHeaders}, {Key: "fields", Input: InputArea},
			{Key: "units", Input: InputArea}, {Key: "thresholds", Input: InputArea}},
		Decode: decodeCustomAPI, View: customAPIView,
		Queries: one("body", "json_api", func(cfg CustomAPIConfig) map[string]any {
			return map[string]any{"url": cfg.URL, "headers": cfg.Headers}
		})}.add()

	Tile[ListConfig]{Key: "list", Category: CategoryStart, Topic: TopicOverview,
		Fields:  []Field{{Key: "entries", Input: InputArea}, {Key: "two_columns", Input: InputCheck}},
		Renames: []rename{{from: "columns", to: "two_columns", value: func(v any) (any, bool) { return v == "2", true }}},
		Decode:  decodeList}.add()

	Tile[HolidaysConfig]{Key: "holidays", Detail: holidaysDetail, Category: CategoryStart, Topic: TopicWorld, RefreshS: 12 * hour,
		Fields: []Field{{Key: "country", Input: InputText, Default: defaultCountry}, {Key: "state", Input: InputText},
			{Key: "bridges", Input: InputCheck}, {Key: "limit", Input: InputNumber, Default: 5, Min: "1", Max: "30"}},
		Decode: decodeHolidays, View: holidaysView,
		Queries: one("days", "holidays", func(cfg HolidaysConfig) map[string]any {
			return map[string]any{"country": cfg.Country, "state": cfg.State}
		}),
		DetailQueries: func(HolidaysConfig) []Query { return []Query{kimaiPeer} }}.add()

	Tile[PictureConfig]{Key: "xkcd", Detail: pictureDetail, Template: "widgets/picture", Category: CategoryStart, Topic: TopicMedia, RefreshS: 6 * hour,
		Fields: []Field{{Key: "random", Input: InputCheck}, {Key: "image_only", Input: InputCheck}},
		Decode: decodePicture, View: pictureView,
		Queries: one("picture", "xkcd", func(cfg PictureConfig) map[string]any { return map[string]any{"random": cfg.Random} })}.add()

	Tile[PictureConfig]{Key: "apod", Detail: pictureDetail, Template: "widgets/picture", Category: CategoryStart, Topic: TopicMedia, RefreshS: 6 * hour,
		Fields: []Field{{Key: "api_key", Input: InputSecret}, {Key: "image_only", Input: InputCheck}},
		Decode: decodePicture, View: pictureView,
		Queries:       one("picture", "apod", func(cfg PictureConfig) map[string]any { return map[string]any{"api_key": cfg.APIKey} }),
		DetailQueries: one(openName, "apod.archive", func(cfg PictureConfig) map[string]any { return map[string]any{"api_key": cfg.APIKey} })}.add()

	Tile[JokeConfig]{Key: "joke", Category: CategoryStart, Topic: TopicMedia, RefreshS: hour,
		Fields: []Field{sel("category", "Any", "Any", "Programming", "Misc", "Pun", "Spooky", "Christmas"), sel("lang", "de", "de", "en"),
			{Key: "every", Input: InputNumber, Min: "0", Max: "168"}},
		Decode: decodeJoke,
		Queries: one("joke", "jokes", func(cfg JokeConfig) map[string]any {
			return map[string]any{"category": cfg.Category, "lang": cfg.Lang, "fresh": freshBucket(cfg.RefreshSeconds())}
		})}.add()

	Tile[CryptoConfig]{Key: "crypto", Detail: cryptoDetail, Category: CategoryStart, Topic: TopicWorld, RefreshS: 10 * minute,
		Fields: []Field{{Key: "coins", Input: InputList, Default: anyList(defaultCoins)}, {Key: "currency", Input: InputText, Default: defaultCurrency},
			{Key: "spark", Input: InputCheck}, {Key: "digits", Input: InputNumber, Default: cryptoDigits, Min: "0", Max: "8"}},
		Decode: decodeCrypto,
		Queries: one("prices", "crypto", func(cfg CryptoConfig) map[string]any {
			return map[string]any{"coins": cfg.Coins, "currency": cfg.Currency, "spark": cfg.Spark}
		}),
		DetailQueries: one(openName, "crypto.history", func(cfg CryptoConfig) map[string]any {
			return map[string]any{"coins": cfg.Coins, "currency": cfg.Currency}
		})}.add()

	Tile[StocksConfig]{Key: "stocks", Detail: stocksDetail, Category: CategoryStart, Topic: TopicWorld, RefreshS: 15 * minute,
		Fields:  []Field{{Key: "tickers", Input: InputList, Default: anyList(defaultTickers)}, sel("change", "day", "day", "week"), {Key: "spark", Input: InputCheck}},
		Decode:  decodeStocks,
		Queries: one("quotes", "stocks", func(cfg StocksConfig) map[string]any { return map[string]any{"symbols": cfg.Symbols} })}.add()

	Tile[FlightsConfig]{Key: "flights", Detail: flightsDetail, Template: "widgets/board", Category: CategoryStart, Topic: TopicWorld, RefreshS: 10 * minute,
		Fields: []Field{{Key: "airport", Input: InputText, Required: true}, sel("direction", "Departure", "Departure", "Arrival"),
			{Key: "airlines", Input: InputList}, {Key: "limit", Input: InputNumber, Default: defaultListLimit, Min: "1", Max: "30"}, {Key: "api_key", Input: InputSecret}},
		Decode: decodeFlights, View: flightsView,
		Queries: one("board", "flights", func(cfg FlightsConfig) map[string]any {
			return map[string]any{"airport": cfg.Airport, "direction": cfg.Direction, "api_key": cfg.APIKey}
		})}.add()

	Tile[TransitConfig]{Key: "transit", Detail: transitDetail, Template: "widgets/board", Category: CategoryStart, Topic: TopicWorld, RefreshS: minute,
		Fields: []Field{{Key: "stop", Input: InputText, Required: true}, {Key: "lines", Input: InputList},
			{Key: "walk", Input: InputNumber, Default: 0, Min: "0", Max: "60"}, {Key: "limit", Input: InputNumber, Default: defaultListLimit, Min: "1", Max: "30"}},
		Decode: decodeTransit, View: transitView,
		Queries: one("board", "transit", func(cfg TransitConfig) map[string]any {
			return map[string]any{"stop": cfg.Stop, "results": float64(boardFetch(cfg.Limit, len(cfg.Lines) > 0 || cfg.Walk > 0))}
		})}.add()
}
