// Package widgets: start page widgets (Dashy replacements).
package widgets

import (
	"cmp"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const defaultTimezone = "Europe/Berlin"

// StatusMode selects whether a link tile checks its target's HTTP status.
type StatusMode string

const (
	StatusOff  StatusMode = "off"
	StatusHTTP StatusMode = "http"
)

// scriptSchemes run code when followed: never a link target.
var scriptSchemes = map[string]bool{"javascript": true, "vbscript": true, "data": true}

// webURL is a link target from config, "" for a script URL. Browsers
// ignore tabs, newlines and case in a scheme ("Java\tScript:"), so the
// check does too; other schemes (ssh:, smb:) and paths pass.
func webURL(raw any) string {
	v := strings.TrimSpace(asString(raw))
	scheme, _, found := strings.Cut(v, ":")
	if !found {
		return v
	}
	scheme = strings.ToLower(strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || r == ' ' {
			return -1
		}
		return r
	}, scheme))
	if scriptSchemes[scheme] {
		return ""
	}
	return v
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func asInt(v any, def int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return def
	}
}

func asFloat(v any) float64 {
	f, _ := v.(float64)
	return f
}

func asStringList(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// anyList turns strings into a config list: ["a"] → []any{"a"}.
func anyList(list []string) []any {
	out := make([]any, len(list))
	for i, s := range list {
		out[i] = s
	}
	return out
}

func asIntList(v any) []int {
	list, _ := v.([]any)
	out := make([]int, 0, len(list))
	for _, item := range list {
		out = append(out, asInt(item, 0))
	}
	return out
}

// LinkConfig is the "link" widget's config: a Dashy-style tile.
type LinkConfig struct {
	URL         string
	Description string
	Icon        string
	Target      enums.LinkTarget
	Status      StatusMode
	StatusURL   string
	Accept      []int
	Insecure    bool
	Hotkey      string
	InfoConn    string // connection key for the info line, "" if none
	Tags        []string
	Items       []SubLink         // more links in the same tile
	Color       TileColor         // "" = theme default
	Headers     map[string]string // sent with the status check
	Method      string            // status check: GET or HEAD
	TimeoutS    float64           // status check limit in seconds, 0 = default
	IconSize    string            // "small", "normal", "large"
}

// SubLink is one extra link inside a link tile, e.g. "Admin" next to the
// main URL.
type SubLink struct {
	Title, URL, Icon string
}

// TileColor names a theme color token, never a raw value.
type TileColor string

// TileColors are the selectable accents; each maps to var(--<name>).
var TileColors = []TileColor{"yellow", "green", "red", "blue", "purple", "aqua", "orange"}

func tileColor(raw any) TileColor {
	want := TileColor(asString(raw))
	for _, c := range TileColors {
		if c == want {
			return c
		}
	}
	return ""
}

func subLinks(raw any) []SubLink {
	list, _ := raw.([]any)
	var out []SubLink
	for _, item := range list {
		m, _ := item.(map[string]any)
		link := SubLink{Title: asString(m["title"]), URL: webURL(m["url"]), Icon: asString(m["icon"])}
		if link.URL == "" {
			continue
		}
		if link.Title == "" {
			link.Title = link.URL
		}
		out = append(out, link)
	}
	return out
}

func stringMap(raw any) map[string]string {
	m, _ := raw.(map[string]any)
	if len(m) == 0 {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = asString(v)
	}
	return out
}

// decodeLink keeps a target or status it does not know, as stored.
func decodeLink(r Raw) LinkConfig {
	cfg := LinkConfig{
		URL: r.URL("url"), Description: r.String("description"), Icon: r.String("icon"),
		Target: enums.LinkTarget(textOr(r, "target")), Status: StatusMode(textOr(r, "status")), StatusURL: r.URL("status_url"),
		Accept: r.Ints("accept"), Insecure: r.Bool("insecure"), Hotkey: r.String("hotkey"),
		Tags: r.List("tags"), Items: subLinks(r.Get("items")), Color: tileColor(r.Get("color")), Headers: stringMap(r.Get("headers")),
		Method: r.Pick("status_method"), TimeoutS: r.Float("status_timeout"), IconSize: r.Pick("icon_size"),
	}
	if info, ok := r.Get("info").(map[string]any); ok {
		cfg.InfoConn = asString(info["connection"])
	}
	return cfg
}

func linkQueries(cfg LinkConfig) []Query {
	var found []Query
	if cfg.Status == StatusHTTP {
		found = append(found, Query{Name: "status", Source: "http_status", Params: map[string]any{
			"url": cmp.Or(cfg.StatusURL, cfg.URL), "accept": cfg.Accept, "insecure": cfg.Insecure, "headers": cfg.Headers,
			"method": cfg.Method, "timeout": cfg.TimeoutS,
		}})
	}
	if cfg.InfoConn != "" {
		found = append(found, Query{Name: "info", Source: "data", Conn: ConnInfo})
	}
	return found
}

// linkView turns the info connection's dataset into the tile's info line,
// e.g. Kimai -> ["3,5 h heute", "Timer läuft"].
func linkView(_ LinkConfig, results map[string]any, ctx ViewCtx) map[string]any {
	info := results["info"]
	if info == nil || ctx.Service == "" {
		return map[string]any{}
	}
	today, err := time.Parse(time.DateOnly, ctx.Today)
	if err != nil {
		today = time.Now().UTC()
	}

	var parts []metrics.InfoPart
	switch data := info.(type) {
	case *sources.KimaiDataset:
		parts = metrics.KimaiInfo(data, today)
	case *sources.NinjaDataset:
		parts = metrics.NinjaInfo(data, today)
	case *sources.SnipeDataset:
		parts = metrics.SnipeInfo(data, today)
	case *sources.DawarichDataset:
		parts = metrics.DawarichInfo(data.LastPoint)
	case *sources.GlancesResult:
		parts = metrics.GlancesInfo(data.CPU)
	case *sources.KumaDataset:
		parts = metrics.KumaInfo(data)
	case *sources.ProxmoxDataset:
		parts = metrics.ProxmoxInfo(data)
	case *sources.PaperlessDataset:
		parts = metrics.PaperlessInfo(data)
	case *sources.CertDataset:
		parts = metrics.CertsInfo(data, today)
	case *sources.ScrutinyDataset:
		parts = metrics.ScrutinyInfo(data)
	case *sources.ImmichDataset:
		parts = metrics.ImmichInfo(data)
	case *sources.UmamiDataset:
		parts = metrics.UmamiInfo(data)
	case *sources.FreshRSSDataset:
		parts = metrics.FreshRSSInfo(data)
	case *sources.GiteaDataset:
		parts = metrics.GiteaInfo(data)
	case *sources.BorgDataset:
		parts = metrics.BorgInfo(data, time.Now().UTC())
	case *sources.HealthchecksDataset:
		parts = metrics.HealthchecksInfo(data)
	case *sources.PrometheusDataset:
		parts = metrics.PrometheusInfo(data)
	case *sources.NVDDataset:
		parts = metrics.NVDInfo(data)
	case *sources.DroneDataset:
		parts = metrics.DroneInfo(data)
	case *sources.WUDDataset:
		parts = metrics.WUDInfo(data)
	case *sources.UPSDataset:
		parts = metrics.UPSInfo(data)
	case *sources.RoutesDataset:
		parts = metrics.RoutesInfo(data)
	case *sources.PlayDataset:
		parts = metrics.PlayInfo(data)
	case *sources.SeerrDataset:
		parts = metrics.SeerrInfo(data)
	case *sources.FritzDataset:
		parts = metrics.FritzInfo(data)
	case *sources.SolarDataset:
		parts = metrics.SolarInfo(data)
	case *sources.WatchtowerDataset:
		parts = metrics.WatchtowerInfo(data)
	case *sources.HassDataset:
		parts = metrics.HassInfo(data)
	case *sources.SureDataset:
		parts = metrics.SureInfo(data)
	case *sources.LinkwardenDataset:
		parts = metrics.LinkwardenInfo(data)
	case *sources.KintsugiDataset:
		parts = metrics.KintsugiInfo(data)
	case *sources.WallosDataset:
		parts = metrics.WallosInfo(data)
	case *sources.MailDataset:
		parts = metrics.MailInfo(data)
	case *sources.TrueNASDataset:
		parts = metrics.TrueNASInfo(data)
	case *sources.KomodoDataset:
		parts = metrics.KomodoInfo(data)
	case *sources.PangolinDataset:
		parts = metrics.PangolinInfo(data)
	case *sources.AuthentikDataset:
		parts = metrics.AuthentikInfo(data)
	case *sources.DNSFilterDataset:
		parts = metrics.DNSFilterInfo(data)
	case *sources.NextcloudDataset:
		parts = metrics.NextcloudInfo(data)
	case *sources.SabnzbdDataset:
		parts = metrics.SabnzbdInfo(data)
	case *sources.GluetunDataset:
		parts = metrics.GluetunInfo(data)
	case *sources.DomainsDataset:
		parts = metrics.DomainsInfo(data, today)
	case *sources.BlacklistDataset:
		parts = metrics.BlacklistInfo(data)
	case *sources.TailscaleDataset:
		parts = metrics.TailscaleInfo(data)
	case *sources.GatewayDataset:
		parts = metrics.GatewayInfo(data)
	case *sources.MediaServerDataset:
		parts = metrics.MediaServerInfo(data)
	case *sources.ArrDataset:
		parts = metrics.ArrInfo(data)
	case *sources.VaultwardenDataset:
		parts = metrics.VaultwardenInfo(data)
	case *sources.SpeedtestDataset:
		parts = metrics.SpeedtestInfo(data)
	case *sources.GrocyDataset:
		parts = metrics.GrocyInfo(data)
	case *sources.DWDDataset:
		parts = metrics.DWDInfo(data)
	case *sources.GitHubDataset:
		parts = metrics.GitHubInfo(data)
	case *sources.TibberDataset:
		parts = metrics.TibberInfo(data)
	}
	return map[string]any{"Info": parts}
}

// RssConfig is the "rss" widget's config.
type RssConfig struct {
	URL     string
	More    []string // further feeds, merged newest first
	Limit   int
	Summary bool
	Images  bool
	MaxAge  int    // days, 0 = any age
	Compact bool   // one line per item: title and day only
	Height  string // rssHeightAuto, or short/medium/tall: fixed, scrolls inside
}

// rssHeightAuto lets the list grow with its items.
const rssHeightAuto = "auto"

func decodeRss(r Raw) RssConfig {
	cfg := RssConfig{URL: r.URL("url"), Limit: r.Int("limit"), Summary: r.Bool("summary"), Images: r.Bool("images"),
		MaxAge: r.Int("max_age"), Compact: r.Bool("titles_only"), Height: r.Pick("list_height")}
	for _, u := range r.List("more_urls") {
		if u = webURL(u); u != "" {
			cfg.More = append(cfg.More, u)
		}
	}
	return cfg
}

// ClockConfig is the "clock" widget's config.
type ClockConfig struct {
	Timezones []string
	Seconds   bool
	Date      bool
	H12       bool // 3:04 PM instead of 15:04
	Analog    bool // a face with hands
}

// ArticleSlot holds the page of the entry a feed dialog shows.
const ArticleSlot = openName

// rssParams are the feed's query params, shared by its dialog's article.
func rssParams(cfg RssConfig) map[string]any {
	return map[string]any{"url": cfg.URL, "limit": float64(cfg.Limit), "urls": cfg.More, "images": cfg.Images, "max_age": float64(cfg.MaxAge)}
}

// OffersDetail: the dialog compares zones, one zone needs none.
func (c ClockConfig) OffersDetail() bool { return len(c.Timezones) > 1 }

// decodeClock keeps the zones Go knows: a typo would show "?".
func decodeClock(r Raw) ClockConfig {
	var tz []string
	for _, name := range r.List("timezones") {
		if _, err := time.LoadLocation(name); err == nil {
			tz = append(tz, name)
		}
	}
	if len(tz) == 0 {
		tz = asStringList(r.field("timezones").Default)
	}
	return ClockConfig{Timezones: tz, Seconds: r.Bool("seconds"), Date: r.Bool("date"), H12: r.Pick("format") == "12", Analog: r.Bool("analog")}
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

// WeatherKind names a WMO weather code's condition ("rain"), the
// catalog key weather.<kind>.
func WeatherKind(code int) string {
	for _, t := range weatherThresholds {
		if code <= t.max {
			return t.kind
		}
	}
	return "unknown"
}

// weatherView drops today from the forecast (the current conditions
// already show it) so the template only lists the days ahead.
func weatherView(cfg WeatherConfig, results map[string]any, _ ViewCtx) map[string]any {
	data, ok := results["weather"].(*sources.WeatherResult)
	if !ok || data == nil {
		return map[string]any{}
	}
	conv := func(c float64) float64 { return c }
	if cfg.Fahrenheit {
		conv = fahrenheit
	}
	var ahead []sources.WeatherDay
	if len(data.Days) > 1 {
		for _, d := range data.Days[1:min(len(data.Days), cfg.Days+1)] {
			d.Max, d.Min = conv(d.Max), conv(d.Min)
			ahead = append(ahead, d)
		}
	}
	out := map[string]any{"Temp": conv(data.Temp), "Code": data.Code, "Label": cfg.Label, "Days": ahead, "Fahrenheit": cfg.Fahrenheit}

	// The next hours: temperature as a line, rain chance as columns, and
	// when rain gets likely.
	if len(data.Hours) >= 2 && cfg.Hourly {
		temps := make([]float64, len(data.Hours))
		rain := make([]int, len(data.Hours))
		for i, h := range data.Hours {
			temps[i], rain[i] = h.Temp, int(h.Rain)
			if _, found := out["RainFrom"]; !found && h.Rain >= rainLikely && len(h.At) >= len("2006-01-02T15:04") {
				out["RainFrom"] = h.At[len("2006-01-02T"):]
			}
		}
		out["Spark"], out["Rain"] = SparkOf(temps), rain
	}
	return out
}

// rainLikely is the rain chance (%) from which the tile says "rain from".
const rainLikely = 50

// WeatherConfig is the "weather" widget's config.
type WeatherConfig struct {
	Label      string
	Lat        float64
	Lon        float64
	Fahrenheit bool
	Hourly     bool // the next hours as a line
	Days       int  // days after today
}

// weatherDays is how many days after today the tile shows by default.
const weatherDays = 3

func decodeWeather(r Raw) WeatherConfig {
	return WeatherConfig{Label: r.String("label"), Lat: r.Float("lat"), Lon: r.Float("lon"),
		Fahrenheit: r.Pick("unit") == "f", Hourly: r.Bool("hourly"), Days: r.Int("days")}
}

// fahrenheit converts °C.
func fahrenheit(c float64) float64 { return c*9/5 + 32 }

// IframeConfig is the "iframe" widget's config.
type IframeConfig struct {
	URL     string
	Height  int
	ReloadM int // minutes between reloads in the browser, 0 = never
}

func decodeIframe(r Raw) IframeConfig {
	return IframeConfig{URL: r.URL("url"), Height: r.Int("height"), ReloadM: r.Int("reload")}
}

// NoteConfig is the "note" widget's config.
type NoteConfig struct {
	Text     string
	Markdown bool
	Blocks   []MDBlock // the text parsed, when Markdown
	Color    string    // background tint, "none" = plain
}

func decodeNote(r Raw) NoteConfig {
	cfg := NoteConfig{Text: r.String("text"), Markdown: r.Bool("markdown"), Color: r.Pick("color")}
	if cfg.Markdown {
		cfg.Blocks = parseMarkdown(cfg.Text)
	}
	return cfg
}

// SysinfoConfig is the "sysinfo" widget's config.
type SysinfoConfig struct {
	Hide map[string]bool // cpu, mem, swap, disks
	Warn float64         // percent from which a meter turns yellow; red 20 points above
}

var sysParts = []string{"cpu", "mem", "swap", "disks"}

// decodeSysinfo reads a warning outside (0, 100] as the default, not
// as the nearest bound.
func decodeSysinfo(r Raw) SysinfoConfig {
	cfg := SysinfoConfig{Hide: map[string]bool{}, Warn: asFloat(r.Get("warn_pct"))}
	if cfg.Warn <= 0 || cfg.Warn > pctFull {
		cfg.Warn = number(r.field("warn_pct").Default)
	}
	for _, p := range sysParts {
		if !r.Bool("show_" + p) {
			cfg.Hide[p] = true
		}
	}
	return cfg
}

// Meter is one labelled percent: Label is a catalog key unless Raw.
type Meter struct {
	Label string
	Raw   bool
	V     float64
	Tier  string
}

// redFrom is where red starts above a warning: 20 points higher, but
// never beyond halfway to full (warn 90 → red 95).
func redFrom(warn float64) float64 {
	return min(warn+loadHigh-loadWarn, (warn+pctFull)/2)
}

func meterTier(v, warn float64) string {
	switch {
	case v >= redFrom(warn):
		return "red"
	case v >= warn:
		return "yellow"
	default:
		return "green"
	}
}

func sysinfoView(cfg SysinfoConfig, results map[string]any, _ ViewCtx) map[string]any {
	// No config (a nil one): the default warning.
	if cfg.Warn == 0 {
		cfg.Warn = loadWarn
	}
	s, ok := results["stats"].(*sources.GlancesResult)
	if !ok {
		return map[string]any{}
	}
	var meters []Meter
	for _, m := range []struct {
		key string
		v   float64
	}{{"cpu", s.CPU}, {"mem", s.Mem}, {"swap", s.Swap}} {
		if !cfg.Hide[m.key] {
			meters = append(meters, Meter{Label: "sys." + m.key, V: m.v, Tier: meterTier(m.v, cfg.Warn)})
		}
	}
	if !cfg.Hide["disks"] {
		for _, d := range s.Disks {
			meters = append(meters, Meter{Label: d.Mount, Raw: true, V: d.Percent, Tier: meterTier(d.Percent, cfg.Warn)})
		}
	}
	return map[string]any{"Meters": meters, "Load": s.Load}
}

// PublicIPConfig is the "public_ip" widget's config.
type PublicIPConfig struct {
	V6    bool
	Watch bool // point out a new address
}

func decodePublicIP(r Raw) PublicIPConfig {
	return PublicIPConfig{V6: r.Bool("ipv6"), Watch: r.Bool("watch")}
}

// IPSeen is the address a viewer last saw and when it changed; the widgets
// service keeps it (results[IPSeenSlot]).
type IPSeen struct {
	IP, Prev string
	Since    time.Time // zero: not seen changing yet
}

// IPSeenSlot carries IPSeen for ExtraIPWatch.
const IPSeenSlot = "ip_seen"

// ipFresh is how long a change is pointed out.
const ipFresh = 24 * time.Hour

func publicIPView(cfg PublicIPConfig, results map[string]any, _ ViewCtx) map[string]any {
	ip, ok := results["ip"].(*sources.PublicIPResult)
	if !ok {
		return map[string]any{}
	}
	out := map[string]any{"IP": ip.IP, "IPv6": ip.IPv6}
	if seen, ok := results[IPSeenSlot].(IPSeen); ok && cfg.Watch && seen.Prev != "" && seen.IP == ip.IP && time.Since(seen.Since) < ipFresh {
		out["Changed"], out["Prev"], out["Since"] = true, seen.Prev, seen.Since
	}
	return out
}

// WatchesIP says whether the widgets service should track the address.
func (c PublicIPConfig) WatchesIP() bool { return c.Watch }

func init() {
	Tile[LinkConfig]{Key: "link", Category: CategoryStart, Topic: TopicOverview, Inline: true, DataChoice: true,
		Fields: []Field{
			{Key: "url", Input: InputText, Required: true},
			{Key: "description", Input: InputArea},
			{Key: "icon", Input: InputText},
			sel("icon_size", "normal", "small", "normal", "large"),
			sel("color", "none", "none", "yellow", "green", "red", "blue", "purple", "aqua", "orange"),
			{Key: "tags", Input: InputList},
			{Key: "items", Input: InputLinks},
			sel("target", "newtab", "newtab", "sametab"),
			{Key: "hotkey", Input: InputText},
			{Key: "info.connection", Input: InputConn},
			sel("status", "http", "http", "off"),
			{Key: "status_url", Input: InputText},
			sel("status_method", "GET", "GET", "HEAD"),
			{Key: "status_timeout", Input: InputNumber, Min: "0", Max: "60"},
			{Key: "accept", Input: InputNumbers},
			{Key: "headers", Input: InputHeaders},
			{Key: "insecure", Input: InputCheck},
		},
		Decode: decodeLink, Queries: linkQueries, View: linkView}.add()

	Tile[RssConfig]{Key: "rss", Width: WidthFull, Detail: rssDetail, Category: CategoryStart, Topic: TopicMedia, RefreshS: 30 * 60,
		Fields: []Field{{Key: "url", Input: InputText, Required: true}, {Key: "more_urls", Input: InputList},
			{Key: "limit", Input: InputNumber, Default: 8, Min: "1", Max: "50"}, {Key: "max_age", Input: InputNumber, Min: "0", Max: "365"},
			{Key: "titles_only", Input: InputCheck}, {Key: "summary", Input: InputCheck}, {Key: "images", Input: InputCheck},
			sel("list_height", rssHeightAuto, rssHeightAuto, "short", "medium", "tall")},
		Decode: decodeRss, Queries: func(cfg RssConfig) []Query {
			return []Query{{Name: "feed", Source: "rss", Params: rssParams(cfg)}}
		},
		DetailQueries: func(cfg RssConfig) []Query {
			return []Query{{Name: openName, Source: "rss.article", Params: rssParams(cfg)}}
		}}.add()

	Tile[ClockConfig]{Key: "clock", Detail: clockDetail, Category: CategoryStart, Topic: TopicOverview, Inline: true, RefreshS: 30,
		Fields: []Field{{Key: "timezones", Input: InputList, Default: []any{defaultTimezone}}, {Key: "seconds", Input: InputCheck}, {Key: "date", Input: InputCheck, Default: true},
			sel("format", "24", "24", "12"), {Key: "analog", Input: InputCheck}},
		Decode: decodeClock}.add()

	Tile[WeatherConfig]{Key: "weather", Detail: weatherDetail, Category: CategoryStart, Topic: TopicHome, RefreshS: 30 * 60,
		Fields: []Field{{Key: placeKey, Input: InputPlace, Required: true}, {Key: "label", Input: InputText}, sel("unit", "c", "c", "f"),
			{Key: "hourly", Input: InputCheck, Default: true}, {Key: "days", Input: InputNumber, Default: weatherDays, Min: "0", Max: "7"}},
		Decode: decodeWeather, View: weatherView, Queries: func(cfg WeatherConfig) []Query {
			return []Query{{Name: "weather", Source: "open_meteo", Params: map[string]any{"lat": cfg.Lat, "lon": cfg.Lon, "days": float64(cfg.Days)}}}
		}}.add()

	Tile[IframeConfig]{Key: "iframe", Width: WidthFull, Detail: iframeDetail, Category: CategoryStart, Topic: TopicDev, Inline: true,
		Fields: []Field{{Key: "url", Input: InputText, Required: true}, {Key: "height", Input: InputNumber, Default: 320, Min: "80", Max: "2000"},
			{Key: "reload", Input: InputNumber, Min: "0", Max: "1440"}},
		Decode: decodeIframe}.add()

	Tile[SysinfoConfig]{Key: "sysinfo", Detail: sysinfoDetail, DetailQueries: openQuery[SysinfoConfig]("glances.detail"), Category: CategoryStart, Topic: TopicHomelab, Service: enums.ServiceGlances, RefreshS: 60, Live: true, DataChoice: true,
		Fields: []Field{{Key: "show_cpu", Input: InputCheck, Default: true}, {Key: "show_mem", Input: InputCheck, Default: true},
			{Key: "show_swap", Input: InputCheck, Default: true}, {Key: "show_disks", Input: InputCheck, Default: true},
			{Key: "warn_pct", Input: InputNumber, Default: loadWarn, Min: "1", Max: "100"}},
		Decode: decodeSysinfo, View: sysinfoView,
		Queries: func(SysinfoConfig) []Query { return []Query{{Name: "stats", Source: "glances", Conn: ConnWidget}} }}.add()

	Tile[PublicIPConfig]{Key: "public_ip", Detail: publicIPDetail, DetailQueries: domainsResolve, Category: CategoryStart, Topic: TopicNetwork, RefreshS: 60 * 60, Extra: ExtraIPWatch,
		Fields: []Field{{Key: "ipv6", Input: InputCheck}, {Key: "watch", Input: InputCheck}},
		Decode: decodePublicIP, View: publicIPView, Queries: func(cfg PublicIPConfig) []Query {
			return []Query{{Name: "ip", Source: "public_ip", Params: map[string]any{"v6": cfg.V6}}}
		}}.add()

	Tile[NoteConfig]{Key: "note", Width: WidthFull, Category: CategoryStart, Topic: TopicOverview, Inline: true,
		Fields: []Field{{Key: "text", Input: InputArea}, {Key: "markdown", Input: InputCheck}, sel("color", "none", accentColors...)},
		Decode: decodeNote}.add()
}
