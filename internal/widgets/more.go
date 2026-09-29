package widgets

// Start widgets without an own service (Dashy's widget list): calendar,
// custom JSON API, static list, holidays, xkcd, NASA picture, jokes,
// crypto, stocks, flights and public transport.

import (
	"cmp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/sources"
)

const (
	defaultListLimit = 8
	defaultCalDays   = 14
	maxCalDays       = 90
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

func decodeCalendar(raw map[string]any) any {
	cfg := CalendarConfig{URL: webURL(raw["ical_url"]), Days: clampInt(asInt(raw["days"], defaultCalDays), 1, maxCalDays),
		Limit: clampInt(asInt(raw["limit"], defaultListLimit), 1, 50), HideAllDay: asBool(raw["hide_all_day"])}
	for i := 2; i <= calendarSlots; i++ {
		cfg.More = append(cfg.More, webURL(raw["ical_url_"+strconv.Itoa(i)]))
	}
	for i := 1; i <= calendarSlots; i++ {
		cfg.Colors = append(cfg.Colors, oneOfStr(raw["color_"+strconv.Itoa(i)], accentColors, "none"))
	}
	return cfg
}

// calendarQueries asks each configured calendar; the first answers as
// "events", the others as "events2", "events3".
func calendarQueries(c any) []Query {
	cfg := c.(CalendarConfig)
	q := []Query{{Name: "events", Source: "ical", Params: map[string]any{"url": cfg.URL, "days": float64(cfg.Days)}}}
	for i, u := range cfg.More {
		if u != "" {
			q = append(q, Query{Name: "events" + strconv.Itoa(i+2), Source: "ical", Params: map[string]any{"url": u, "days": float64(cfg.Days)}})
		}
	}
	return q
}

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

func decodeCustomAPI(raw map[string]any) any {
	cfg := CustomAPIConfig{URL: webURL(raw["url"]), Headers: stringMap(raw["headers"]), Thresholds: parseThresholds(asString(raw["thresholds"])),
		Units: parsePairs(asString(raw["units"]))}
	for _, line := range strings.Split(asString(raw["fields"]), "\n") {
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

func decodeList(raw map[string]any) any {
	cfg := ListConfig{TwoCols: asBool(raw["two_columns"])}
	for _, line := range strings.Split(asString(raw["entries"]), "\n") {
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
func decodeHolidays(raw map[string]any) any {
	country := cmp.Or(strings.ToUpper(asString(raw["country"])), defaultCountry)
	state := strings.ToUpper(strings.TrimSpace(asString(raw["state"])))
	if state != "" && !strings.Contains(state, "-") {
		state = country + "-" + state
	}
	return HolidaysConfig{Country: country, State: state, Limit: clampInt(asInt(raw["limit"], 5), 1, 30),
		Bridges: asBool(raw["bridges"])}
}

type JokeConfig struct {
	Category, Lang string
	EveryH         int // hours until the next joke, 0 = the default hour
}

func decodeJoke(raw map[string]any) any {
	return JokeConfig{Category: cmp.Or(asString(raw["category"]), "Any"), Lang: cmp.Or(asString(raw["lang"]), "de"),
		EveryH: clampInt(asInt(raw["every"], 0), 0, 168)}
}

// RefreshSeconds shows the next joke at the chosen pace.
func (c JokeConfig) RefreshSeconds() int { return c.EveryH * secondsPerMinute * secondsPerMinute }

// PictureConfig is the xkcd and APOD widgets' config.
type PictureConfig struct {
	APIKey    string
	ImageOnly bool // no title and text
	Random    bool // xkcd: any comic instead of the newest
}

func decodePicture(raw map[string]any) any {
	return PictureConfig{APIKey: asString(raw["api_key"]), ImageOnly: asBool(raw["image_only"]), Random: asBool(raw["random"])}
}

func pictureView(cfgAny any, _ map[string]any, _ ViewCtx) map[string]any {
	cfg, _ := cfgAny.(PictureConfig)
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

func decodeCrypto(raw map[string]any) any {
	return CryptoConfig{Coins: listOr(raw["coins"], defaultCoins), Currency: cmp.Or(strings.ToLower(asString(raw["currency"])), defaultCurrency),
		Spark: asBool(raw["spark"]), Digits: clampInt(asInt(raw["digits"], cryptoDigits), 0, 8)}
}

type StocksConfig struct {
	Symbols []string
	Week    bool // change over a week instead of since the last close
	Spark   bool // a month's line
}

func decodeStocks(raw map[string]any) any {
	return StocksConfig{Symbols: listOr(raw["tickers"], defaultTickers), Week: asString(raw["change"]) == "week", Spark: asBool(raw["spark"])}
}

type FlightsConfig struct {
	Airport, Direction, APIKey string
	Limit                      int
	Airlines                   []string // flight number prefixes ("LH", "EW"), upper case; empty = all
}

func decodeFlights(raw map[string]any) any {
	direction := asString(raw["direction"])
	if direction != "Arrival" {
		direction = "Departure"
	}
	return FlightsConfig{Airport: strings.ToUpper(asString(raw["airport"])), Direction: direction,
		APIKey: asString(raw["api_key"]), Limit: clampInt(asInt(raw["limit"], defaultListLimit), 1, 30),
		Airlines: upperList(raw["airlines"])}
}

type TransitConfig struct {
	Stop  string
	Limit int
	Lines []string // line names, upper case; empty = all
	Walk  int      // minutes to the stop; what leaves sooner is left out
}

func decodeTransit(raw map[string]any) any {
	return TransitConfig{Stop: asString(raw["stop"]), Limit: clampInt(asInt(raw["limit"], defaultListLimit), 1, 30),
		Lines: upperList(raw["lines"]), Walk: clampInt(asInt(raw["walk"], 0), 0, 60)}
}

// upperList reads a list field in upper case without spaces ("S 1" = "S1").
func upperList(v any) []string {
	var out []string
	for _, s := range asStringList(v) {
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

func calendarView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg := cfgAny.(CalendarConfig)
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

func customAPIView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg := cfgAny.(CustomAPIConfig)
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

// HolidayRow is one upcoming holiday with the days left.
type HolidayRow struct {
	Day, Name string
	In        int
	Bridge    string // the bridge day, "" if none or not asked
}

func holidaysView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg := cfgAny.(HolidaysConfig)
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
}

func boardView(limit int, data *sources.BoardResult) map[string]any {
	zone := clockZone()
	var rows []MoveRow
	for _, m := range data.Movements {
		if len(rows) >= limit {
			break
		}
		rows = append(rows, MoveRow{Time: m.When.In(zone).Format(timeOfDay), Line: m.Line, Place: m.Place,
			Status: m.Status, Platform: m.Platform, Delay: m.Delay, Canceled: m.Canceled})
	}
	return map[string]any{"Stop": data.Stop, "Rows": rows}
}

func flightsView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg := cfgAny.(FlightsConfig)
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

func transitView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg := cfgAny.(TransitConfig)
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
func one(name, source string, params func(cfg any) map[string]any) QueriesFunc {
	return func(cfg any) []Query {
		return []Query{{Name: name, Source: source, Params: params(cfg)}}
	}
}

func init() {
	const (
		minute = 60
		hour   = 60 * minute
	)

	Register(WidgetType{Key: "calendar", Decode: decodeCalendar, Template: "widgets/calendar", Category: CategoryStart, RefreshS: 15 * minute,
		Queries: calendarQueries, View: calendarView})

	Register(WidgetType{Key: "custom_api", Decode: decodeCustomAPI, Template: "widgets/custom_api", Category: CategoryStart, RefreshS: 5 * minute,
		Queries: one("body", "json_api", func(c any) map[string]any {
			cfg := c.(CustomAPIConfig)
			return map[string]any{"url": cfg.URL, "headers": cfg.Headers}
		}), View: customAPIView})

	Register(WidgetType{Key: "list", Decode: decodeList, Template: "widgets/list", Category: CategoryStart})

	Register(WidgetType{Key: "holidays", Decode: decodeHolidays, Template: "widgets/holidays", Category: CategoryStart, RefreshS: 12 * hour,
		Queries: one("days", "holidays", func(c any) map[string]any {
			cfg := c.(HolidaysConfig)
			return map[string]any{"country": cfg.Country, "state": cfg.State}
		}), View: holidaysView})

	Register(WidgetType{Key: "xkcd", Decode: decodePicture, Template: "widgets/picture", Category: CategoryStart, RefreshS: 6 * hour,
		View: pictureView, Queries: one("picture", "xkcd", func(c any) map[string]any { return map[string]any{"random": c.(PictureConfig).Random} })})

	Register(WidgetType{Key: "apod", Decode: decodePicture, Template: "widgets/picture", Category: CategoryStart, RefreshS: 6 * hour,
		View: pictureView, Queries: one("picture", "apod", func(c any) map[string]any { return map[string]any{"api_key": c.(PictureConfig).APIKey} })})

	Register(WidgetType{Key: "joke", Decode: decodeJoke, Template: "widgets/joke", Category: CategoryStart, RefreshS: hour,
		Queries: one("joke", "jokes", func(c any) map[string]any {
			cfg := c.(JokeConfig)
			return map[string]any{"category": cfg.Category, "lang": cfg.Lang, "fresh": freshBucket(cfg.RefreshSeconds())}
		})})

	Register(WidgetType{Key: "crypto", Decode: decodeCrypto, Template: "widgets/crypto", Category: CategoryStart, RefreshS: 10 * minute,
		Queries: one("prices", "crypto", func(c any) map[string]any {
			cfg := c.(CryptoConfig)
			return map[string]any{"coins": cfg.Coins, "currency": cfg.Currency, "spark": cfg.Spark}
		})})

	Register(WidgetType{Key: "stocks", Decode: decodeStocks, Template: "widgets/stocks", Category: CategoryStart, RefreshS: 15 * minute,
		Queries: one("quotes", "stocks", func(c any) map[string]any { return map[string]any{"symbols": c.(StocksConfig).Symbols} })})

	Register(WidgetType{Key: "flights", Decode: decodeFlights, Template: "widgets/board", Category: CategoryStart, RefreshS: 10 * minute,
		Queries: one("board", "flights", func(c any) map[string]any {
			cfg := c.(FlightsConfig)
			return map[string]any{"airport": cfg.Airport, "direction": cfg.Direction, "api_key": cfg.APIKey}
		}), View: flightsView})

	Register(WidgetType{Key: "transit", Decode: decodeTransit, Template: "widgets/board", Category: CategoryStart, RefreshS: minute,
		Queries: one("board", "transit", func(c any) map[string]any {
			cfg := c.(TransitConfig)
			return map[string]any{"stop": cfg.Stop, "results": float64(boardFetch(cfg.Limit, len(cfg.Lines) > 0 || cfg.Walk > 0))}
		}), View: transitView})
}
