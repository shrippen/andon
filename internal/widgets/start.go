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

// listOr is v as strings, or def when v holds none: a tile needs
// something to show.
func listOr(v any, def []string) []string {
	if list := asStringList(v); len(list) > 0 {
		return list
	}
	return def
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

func decodeLink(raw map[string]any) any {
	target := enums.LinkTarget(asString(raw["target"]))
	if target == "" {
		target = enums.LinkNewTab
	}
	status := StatusMode(asString(raw["status"]))
	if status == "" {
		status = StatusHTTP
	}
	cfg := LinkConfig{
		URL: webURL(raw["url"]), Description: asString(raw["description"]), Icon: asString(raw["icon"]),
		Target: target, Status: status, StatusURL: webURL(raw["status_url"]),
		Accept: asIntList(raw["accept"]), Insecure: asBool(raw["insecure"]), Hotkey: asString(raw["hotkey"]),
		Tags: asStringList(raw["tags"]), Items: subLinks(raw["items"]), Color: tileColor(raw["color"]), Headers: stringMap(raw["headers"]),
		Method: oneOfStr(raw["status_method"], []string{"GET", "HEAD"}, "GET"), TimeoutS: min(max(asFloat(raw["status_timeout"]), 0), 60),
		IconSize: oneOfStr(raw["icon_size"], []string{"small", "normal", "large"}, "normal"),
	}
	if info, ok := raw["info"].(map[string]any); ok {
		cfg.InfoConn = asString(info["connection"])
	}
	return cfg
}

func linkQueries(cfgAny any) []Query {
	cfg := cfgAny.(LinkConfig)
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
func linkView(_ any, results map[string]any, ctx ViewCtx) map[string]any {
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

// decodeClock keeps the zones Go knows: a typo would show "?".
func decodeClock(raw map[string]any) any {
	var tz []string
	for _, name := range asStringList(raw["timezones"]) {
		if _, err := time.LoadLocation(name); err == nil {
			tz = append(tz, name)
		}
	}
	if len(tz) == 0 {
		tz = []string{defaultTimezone}
	}
	date := true
	if v, ok := raw["date"]; ok {
		date = asBool(v)
	}
	return ClockConfig{Timezones: tz, Seconds: asBool(raw["seconds"]), Date: date, H12: raw["format"] == "12", Analog: asBool(raw["analog"])}
}

// weatherView drops today from the forecast (the current conditions
// already show it) so the template only lists the days ahead.
func weatherView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg := cfgAny.(WeatherConfig)
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

func decodeWeather(raw map[string]any) any {
	return WeatherConfig{Label: asString(raw["label"]), Lat: asFloat(raw["lat"]), Lon: asFloat(raw["lon"]),
		Fahrenheit: raw["unit"] == "f", Hourly: boolOr(raw["hourly"], true), Days: clampInt(asInt(raw["days"], weatherDays), 0, 7)}
}

// fahrenheit converts °C.
func fahrenheit(c float64) float64 { return c*9/5 + 32 }

// IframeConfig is the "iframe" widget's config.
type IframeConfig struct {
	URL     string
	Height  int
	ReloadM int // minutes between reloads in the browser, 0 = never
}

func decodeIframe(raw map[string]any) any {
	height := asInt(raw["height"], 320)
	if height < 80 {
		height = 80
	}
	if height > 2000 {
		height = 2000
	}
	return IframeConfig{URL: webURL(raw["url"]), Height: height, ReloadM: clampInt(asInt(raw["reload"], 0), 0, 1440)}
}

// NoteConfig is the "note" widget's config.
type NoteConfig struct {
	Text     string
	Markdown bool
	Blocks   []MDBlock // the text parsed, when Markdown
	Color    string    // background tint, "none" = plain
}

func decodeNote(raw map[string]any) any {
	cfg := NoteConfig{Text: asString(raw["text"]), Markdown: asBool(raw["markdown"]), Color: oneOfStr(raw["color"], accentColors, "none")}
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

func decodeSysinfo(raw map[string]any) any {
	cfg := SysinfoConfig{Hide: map[string]bool{}, Warn: asFloat(raw["warn_pct"])}
	if cfg.Warn <= 0 || cfg.Warn > pctFull {
		cfg.Warn = loadWarn
	}
	for _, p := range sysParts {
		if !boolOr(raw["show_"+p], true) {
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

func sysinfoView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, ok := cfgAny.(SysinfoConfig)
	if !ok {
		cfg = SysinfoConfig{Warn: loadWarn}
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

func decodePublicIP(raw map[string]any) any {
	return PublicIPConfig{V6: asBool(raw["ipv6"]), Watch: asBool(raw["watch"])}
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

func publicIPView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg, _ := cfgAny.(PublicIPConfig)
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
	Register(WidgetType{Key: "link", Decode: decodeLink, Category: CategoryStart, Inline: true, Queries: linkQueries, View: linkView})

	Tile[RssConfig]{Key: "rss", Category: CategoryStart, Topic: TopicMedia, RefreshS: 30 * 60,
		Fields: []Field{{Key: "url", Input: InputText, Required: true}, {Key: "limit", Input: InputNumber, Default: 8, Min: "1", Max: "50"},
			{Key: "summary", Input: InputCheck}, {Key: "more_urls", Input: InputList}, {Key: "images", Input: InputCheck},
			{Key: "max_age", Input: InputNumber, Min: "0", Max: "365"}, {Key: "titles_only", Input: InputCheck},
			sel("list_height", rssHeightAuto, rssHeightAuto, "short", "medium", "tall")},
		Decode: decodeRss, Queries: func(cfg RssConfig) []Query {
			return []Query{{Name: "feed", Source: "rss", Params: map[string]any{"url": cfg.URL, "limit": cfg.Limit, "urls": cfg.More,
				"images": cfg.Images, "max_age": float64(cfg.MaxAge)}}}
		}}.add()

	Register(WidgetType{Key: "clock", Decode: decodeClock, Category: CategoryStart, Inline: true, RefreshS: 30})

	Register(WidgetType{Key: "weather", Decode: decodeWeather, Category: CategoryStart, RefreshS: 30 * 60, View: weatherView, Queries: func(cfgAny any) []Query {
		cfg := cfgAny.(WeatherConfig)
		return []Query{{Name: "weather", Source: "open_meteo", Params: map[string]any{"lat": cfg.Lat, "lon": cfg.Lon, "days": float64(cfg.Days)}}}
	}})

	Register(WidgetType{Key: "iframe", Decode: decodeIframe, Category: CategoryStart, Inline: true})

	Register(WidgetType{Key: "sysinfo", Decode: decodeSysinfo, Category: CategoryStart, Service: enums.ServiceGlances, RefreshS: 60, Live: true, View: sysinfoView,
		Queries: func(any) []Query { return []Query{{Name: "stats", Source: "glances", Conn: ConnWidget}} }})

	Register(WidgetType{Key: "public_ip", Decode: decodePublicIP, Category: CategoryStart, RefreshS: 60 * 60, View: publicIPView, Extra: ExtraIPWatch,
		Queries: func(c any) []Query {
			cfg, _ := c.(PublicIPConfig)
			return []Query{{Name: "ip", Source: "public_ip", Params: map[string]any{"v6": cfg.V6}}}
		}})

	Register(WidgetType{Key: "note", Decode: decodeNote, Category: CategoryStart, Inline: true})
}
