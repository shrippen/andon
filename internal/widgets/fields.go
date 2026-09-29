package widgets

import (
	"andon/internal/metrics"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// Widget config forms are generated from a field list per type:
//
//	rss: [{url text required} {limit number} {summary check}]
//	  form {"cfg.url": ..., "cfg.limit": "8"} → config {"url": ..., "limit": 8}
//
// Dotted keys nest: "info.connection" ↔ {"info": {"connection": ...}}.

// Input is how a config field is edited.
type Input string

const (
	InputText    Input = "text"
	InputArea    Input = "textarea"
	InputNumber  Input = "number"
	InputCheck   Input = "checkbox"
	InputSelect  Input = "select"
	InputList    Input = "list"    // comma separated strings
	InputNumbers Input = "numbers" // comma separated integers
	InputConn    Input = "connection"
	InputLinks   Input = "links"   // one "title | url | icon" per line
	InputHeaders Input = "headers" // one "Name: value" per line
	InputSecret  Input = "secret"  // write-only: stored encrypted, never shown
	InputPlace   Input = "place"   // search by name; stores place, lat and lon
)

const (
	linkSep     = "|"
	headerSep   = ":"
	secretClear = "-" // matches util.SecretClear: drop a stored secret
)

// FormPrefix marks config fields in a form ("cfg.url").
const FormPrefix = "cfg."

// FormMarker (with FormPrefix) says the editor rendered the config fields,
// so an absent checkbox means "off" rather than "keep the default".
const FormMarker = "__fields"

const listSep = ","

// Field is one editable config value.
type Field struct {
	Key      string
	Input    Input
	Options  []string
	Required bool
	Default  any
}

func sel(key string, def string, options ...string) Field {
	return Field{Key: key, Input: InputSelect, Options: options, Default: def}
}

var fieldsByType = map[string][]Field{
	"subscriptions": {{Key: "limit", Input: InputNumber, Default: defaultSubRows}, sel("sort", "next", "next", "price"),
		{Key: "yearly", Input: InputCheck}, {Key: "categories", Input: InputList}},
	"links_down": {{Key: "limit", Input: InputNumber, Default: defaultLinksDown}},
	"timeline_recent": {{Key: "limit", Input: InputNumber, Default: defaultRecent}, {Key: "days", Input: InputNumber, Default: TimelineDays},
		sel("kinds", "all", "all", "updates", "hints")},
	"uptime_month": {{Key: "sla", Input: InputNumber}, {Key: "filter", Input: InputList}},
	"hint_noise":   {sel("period", "14", "14", "30", "90")},
	"hint_trend":   {sel("period", "30", "14", "30", "90")},
	"status_light": {sel("red_from", "critical", "critical", "warn"), sel("yellow_from", "warn", "warn", "info", "off"), {Key: "sources", Input: InputList},
		{Key: "direct", Input: InputCheck}, {Key: "text_green", Input: InputText}, {Key: "text_yellow", Input: InputText}, {Key: "text_red", Input: InputText}},
	"exposure": {{Key: "only_open", Input: InputCheck}},
	"travel": {{Key: "km_rate", Input: InputNumber, Default: defaultKMRate}, sel("period", "month", "month", "year"),
		{Key: "hide_bar", Input: InputCheck}},
	"receipts_missing": {{Key: "days", Input: InputNumber, Default: defaultReceiptDays}, {Key: "limit", Input: InputNumber, Default: defaultReceiptRows},
		{Key: "min_amount", Input: InputNumber}},
	"today": {{Key: "timezone", Input: InputText, Default: defaultTimezone}, {Key: "stop", Input: InputText}, {Key: "days", Input: InputNumber, Default: todayDeadlineDays},
		{Key: "show_calendar", Input: InputCheck, Default: true}, {Key: "show_timer", Input: InputCheck, Default: true},
		{Key: "show_transit", Input: InputCheck, Default: true}, {Key: "show_deadlines", Input: InputCheck, Default: true},
		{Key: "hide_past", Input: InputCheck}},
	"month_close": {sel("month", "previous", "previous", "current"), {Key: "manual", Input: InputCheck},
		{Key: "close_hours", Input: InputCheck, Default: true}, {Key: "close_drafts", Input: InputCheck, Default: true},
		{Key: "close_receipts", Input: InputCheck, Default: true}, {Key: "close_inbox", Input: InputCheck, Default: true},
		{Key: "close_vat", Input: InputCheck, Default: true}},
	"rate_trend": {{Key: "target", Input: InputNumber}, {Key: "months", Input: InputNumber, Default: sparkMonths},
		{Key: "billable_only", Input: InputCheck}},
	"payment_days": {{Key: "target", Input: InputNumber, Default: defaultPayTarget}, {Key: "limit", Input: InputNumber, Default: defaultPayRows},
		{Key: "months", Input: InputNumber}, {Key: "hide_clients", Input: InputList}},
	"link": {
		{Key: "url", Input: InputText, Required: true},
		{Key: "description", Input: InputArea},
		{Key: "icon", Input: InputText},
		sel("target", "newtab", "newtab", "sametab"),
		sel("status", "http", "http", "off"),
		{Key: "status_url", Input: InputText},
		{Key: "accept", Input: InputNumbers},
		{Key: "insecure", Input: InputCheck},
		{Key: "hotkey", Input: InputText},
		{Key: "info.connection", Input: InputConn},
		{Key: "tags", Input: InputList},
		{Key: "items", Input: InputLinks},
		sel("color", "none", "none", "yellow", "green", "red", "blue", "purple", "aqua", "orange"),
		{Key: "headers", Input: InputHeaders},
		sel("status_method", "GET", "GET", "HEAD"),
		{Key: "status_timeout", Input: InputNumber},
		sel("icon_size", "normal", "small", "normal", "large"),
	},
	"rss": {{Key: "url", Input: InputText, Required: true}, {Key: "limit", Input: InputNumber, Default: 8}, {Key: "summary", Input: InputCheck},
		{Key: "more_urls", Input: InputList}, {Key: "images", Input: InputCheck}, {Key: "max_age", Input: InputNumber},
		{Key: "titles_only", Input: InputCheck}, sel("list_height", rssHeightAuto, rssHeightAuto, "short", "medium", "tall")},
	"clock": {{Key: "timezones", Input: InputList, Default: []any{defaultTimezone}}, {Key: "seconds", Input: InputCheck}, {Key: "date", Input: InputCheck, Default: true},
		sel("format", "24", "24", "12"), {Key: "analog", Input: InputCheck}},
	"weather": {{Key: "label", Input: InputText}, {Key: placeKey, Input: InputPlace, Required: true}, sel("unit", "c", "c", "f"),
		{Key: "hourly", Input: InputCheck, Default: true}, {Key: "days", Input: InputNumber, Default: weatherDays}},
	"greeting": {{Key: "label", Input: InputText}, {Key: placeKey, Input: InputPlace},
		{Key: "timezone", Input: InputText, Default: defaultTimezone}, {Key: "since_hour", Input: InputNumber, Default: greetingSinceHour},
		{Key: "show_weather", Input: InputCheck, Default: true}, {Key: "show_since", Input: InputCheck, Default: true},
		{Key: "show_hints", Input: InputCheck, Default: true}},
	"iframe": {{Key: "url", Input: InputText, Required: true}, {Key: "height", Input: InputNumber, Default: 320}, {Key: "reload", Input: InputNumber}},
	"note":   {{Key: "text", Input: InputArea}, {Key: "markdown", Input: InputCheck}, sel("color", "none", accentColors...)},
	"image": {{Key: "url", Input: InputText, Required: true}, {Key: "height", Input: InputNumber, Default: 240}, {Key: "link", Input: InputText},
		{Key: "reload", Input: InputNumber}, sel("fit", "contain", "contain", "cover")},
	"rates": {{Key: "base", Input: InputText, Default: "EUR"}, {Key: "symbols", Input: InputList, Default: anyList(defaultRates)},
		{Key: "change", Input: InputCheck}, {Key: "invert", Input: InputCheck}},
	"monitors": {{Key: "filter", Input: InputList}, sel("days", "14", "7", "14", "30"), {Key: "response_time", Input: InputCheck, Default: true}},
	"hass": {{Key: "entities", Input: InputList, Required: true}, {Key: "labels", Input: InputArea}, {Key: "thresholds", Input: InputArea},
		{Key: "two_columns", Input: InputCheck}},
	"sysinfo": {{Key: "show_cpu", Input: InputCheck, Default: true}, {Key: "show_mem", Input: InputCheck, Default: true},
		{Key: "show_swap", Input: InputCheck, Default: true}, {Key: "show_disks", Input: InputCheck, Default: true},
		{Key: "warn_pct", Input: InputNumber, Default: loadWarn}},
	"glances_chart": {sel("metric", "cpu", "cpu", "mem", "load", "swap"), {Key: "points", Input: InputNumber, Default: defaultGlancesPoints},
		{Key: "warn_line", Input: InputNumber}},
	"public_ip": {{Key: "ipv6", Input: InputCheck}, {Key: "watch", Input: InputCheck}},
	"kpi": {sel("metric", string(MetricRevenueYTD), "hours_today", "hours_week", "hours_month", "utilization", "unbilled",
		"revenue_ytd", "revenue_month", "open_amount", "overdue_amount", "vat_liability", "tax_reserve",
		"asset_value", "assets_ready", "revenue_forecast", "cash_30", "liquidity_30", "effective_rate", "net_worth", "cash", "safe_to_spend"),
		sel("compare", comparePrevYear, comparePrevYear, comparePrevMonth, compareOff),
		{Key: "target_value", Input: InputNumber}, {Key: "spark", Input: InputCheck, Default: true}, {Key: "free", Input: InputCheck}},
	"table": {sel("table", "open_invoices", "open_invoices", "unbilled", "budgets", "client_shares", "asset_dates", "trips", "effective_rates", "app_usage", "payment_morale",
		"full_rates", "unbilled_aging", "payment_matches", "missing_receipts", "subscriptions", "budget_forecast", "project_margins", "exposure", "domain_chain"),
		{Key: "limit", Input: InputNumber, Default: 8}, {Key: "hide_cols", Input: InputList},
		sel("sort", sortAsIs, sortAsIs, sortAmountDesc, sortAmountAsc, sortName, sortDate), {Key: "sum_row", Input: InputCheck}},
	"chart": {sel("chart", "revenue", "revenue", "hours", "seasonal"), {Key: "months", Input: InputNumber, Default: 12},
		{Key: "show_prev", Input: InputCheck, Default: true}, {Key: "values", Input: InputCheck}, {Key: "goal_line", Input: InputCheck}},
	"progress": {{Key: "goal", Input: InputCheck, Default: true}, {Key: "projects", Input: InputList}, {Key: "soll", Input: InputCheck, Default: true},
		{Key: "warn_ahead", Input: InputNumber, Default: 10}},
	"deadlines": {{Key: "days", Input: InputNumber, Default: 45}, {Key: "show_vat", Input: InputCheck, Default: true},
		{Key: "show_prepayment", Input: InputCheck, Default: true}, {Key: "show_annual", Input: InputCheck, Default: true},
		{Key: "amounts", Input: InputCheck, Default: true}},
	"trend": {sel("metric", string(TrendOpenAmount), "revenue_ytd", "open_amount", "month_min"), {Key: "days", Input: InputNumber, Default: 90},
		{Key: "target_value", Input: InputNumber}, {Key: "smooth", Input: InputCheck}},
	"docker_containers": {{Key: "only_problems", Input: InputCheck}},
	"umami_sites":       {{Key: "filter", Input: InputList}},
	"immich_library":    {},
	"money_flow":        {{Key: "show_paid", Input: InputCheck, Default: true}},
	"expiries":          {{Key: "days", Input: InputNumber, Default: 90}, {Key: "limit", Input: InputNumber, Default: 15}, {Key: "sources", Input: InputList}},
	"updates":           {{Key: "limit", Input: InputNumber, Default: 20}, {Key: "sources", Input: InputList}, sel("sort", "urgency", "urgency", "age")},
	"kimai_timer":       {{Key: "recent", Input: InputNumber, Default: recentShown}, {Key: "ask_note", Input: InputCheck}},
	"conn_health":       {{Key: "limit", Input: InputNumber, Default: 4}, {Key: "only_shaky", Input: InputCheck}},
	"kimai_week":        {{Key: "billable_only", Input: InputCheck}},
	"kimai_split":       {sel("week", "this", "this", "last"), sel("group", "customer", "customer", "project")},
	"unbilled_age":      {{Key: "bands", Input: InputNumbers, Default: []any{30.0, 60.0}}, {Key: "hide_internal", Input: InputCheck}, {Key: "hide_clients", Input: InputList}},
	"disks":             {{Key: "temp_warn", Input: InputNumber, Default: tempWarn}, {Key: "only_problems", Input: InputCheck}},
	"komodo_stacks":     {{Key: "filter", Input: InputList}, {Key: "only_issues", Input: InputCheck}},
	"truenas_pools": {{Key: "warn_pct", Input: InputNumber, Default: loadWarn}, {Key: "app_updates", Input: InputCheck},
		{Key: "forecast", Input: InputCheck}},
	"pihole":           {{Key: "top_clients", Input: InputCheck}, {Key: "top_domains", Input: InputCheck}},
	"adguard":          {{Key: "top_clients", Input: InputCheck}, {Key: "top_domains", Input: InputCheck}},
	"vpn":              {{Key: "expected_country", Input: InputText}},
	"gateway":          {{Key: "hide_measures", Input: InputCheck}, {Key: "device_list", Input: InputCheck}},
	"expiry":           {{Key: "max_days", Input: InputNumber}, sel("kinds", "both", "both", "certs", "domains")},
	"speed_history":    {{Key: "days", Input: InputNumber, Default: speedDays}},
	"sabnzbd":          {{Key: "queue", Input: InputNumber, Default: 0}},
	"paperless_inbox":  {{Key: "newest_docs", Input: InputNumber, Default: 0}, {Key: "tag", Input: InputText}},
	"mail_invoices":    {{Key: "only_open", Input: InputCheck}, {Key: "limit", Input: InputNumber, Default: listShown}},
	"freshrss_feeds":   {{Key: "filter", Input: InputList}, {Key: "only_unread", Input: InputCheck, Default: true}},
	"linkwarden":       {{Key: "filter", Input: InputList}, {Key: "newest", Input: InputCheck}},
	"kintsugi":         {{Key: "limit", Input: InputNumber, Default: listShown}, sel("kind", "all", "all", "acquisition", "development")},
	"gitea_reviews":    {sel("show", "all", "all", "reviews", "issues"), {Key: "limit", Input: InputNumber, Default: listShown}},
	"dawarich_day":     {sel("day", "today", "today", "yesterday")},
	"authentik_logins": {sel("span", "7d", "24h", "7d"), {Key: "only_failures", Input: InputCheck}},
	"vaultwarden_2fa":  {{Key: "list_without", Input: InputCheck, Default: true}},
	"invoice_aging":    {{Key: "bands", Input: InputNumbers, Default: []any{30.0, 60.0}}, {Key: "hide_clients", Input: InputList}},
	"heatmap":          {{Key: "months", Input: InputNumber, Default: 12}, {Key: "weekdays", Input: InputCheck}, {Key: "by_goal", Input: InputCheck}},
	"jsonapi":          {{Key: "thresholds", Input: InputArea}, {Key: "units", Input: InputArea}},
	"tailscale":        {{Key: "only_trouble", Input: InputCheck}, {Key: "hide_after", Input: InputNumber, Default: 0}, {Key: "tags", Input: InputList}},
	"mediaserver":      {{Key: "show_users", Input: InputCheck, Default: true}},
	"arr_upcoming":     {{Key: "days", Input: InputNumber, Default: arrDays}},
	"grocy": {{Key: "show_stock", Input: InputCheck, Default: true}, {Key: "show_shopping", Input: InputCheck, Default: true},
		{Key: "show_chores", Input: InputCheck, Default: true}, {Key: "days", Input: InputNumber, Default: 0}},
	"dwd":       {sel("min_level", "minor", "minor", "moderate", "severe", "extreme")},
	"github":    {{Key: "filter", Input: InputList}, {Key: "only_red", Input: InputCheck}},
	"speedtest": {{Key: "ping", Input: InputCheck, Default: true}},
	"energy": {{Key: "power_entity", Input: InputText}, {Key: "cheap_hours", Input: InputNumber, Default: metrics.CheapHours},
		{Key: "tomorrow", Input: InputCheck, Default: true}, sel("price", "total", "total", "energy")},
	"update_window": {{Key: "window", Input: InputText}, {Key: "timezone", Input: InputText, Default: defaultTimezone},
		{Key: "use_backup", Input: InputCheck, Default: true}, {Key: "use_streams", Input: InputCheck, Default: true},
		{Key: "use_timer", Input: InputCheck, Default: true}, {Key: "use_meetings", Input: InputCheck, Default: true},
		{Key: "use_price", Input: InputCheck, Default: true}},
	"homelab_cost": {sel("period", "month", "month", "year"), {Key: "power_split", Input: InputCheck, Default: true}},
	"week_story": {sel("period", "days7", "days7", "calendar"), {Key: "show_hours", Input: InputCheck, Default: true},
		{Key: "show_money", Input: InputCheck, Default: true}, {Key: "show_storage", Input: InputCheck, Default: true},
		{Key: "show_power", Input: InputCheck, Default: true}, {Key: "show_hints", Input: InputCheck, Default: true}},
	"storage_forecast": {{Key: "filter", Input: InputList}, {Key: "ahead", Input: InputNumber, Default: storageAhead}},
	"cashflow": {{Key: "days", Input: InputNumber, Default: defaultCashDays}, {Key: "min_balance", Input: InputNumber},
		{Key: "delay", Input: InputNumber}},
	"backups": {{Key: "max_hours", Input: InputNumber, Default: defaultBackupHours}, {Key: "tools", Input: InputList},
		{Key: "only_problems", Input: InputCheck}, sel("days", "14", "7", "14", "30")},
	"hints": {{Key: "sources", Input: InputList}, {Key: "min_severity", Input: InputNumber, Default: 10}, {Key: "limit", Input: InputNumber, Default: 8},
		{Key: "buttons", Input: InputCheck}, {Key: "by_value", Input: InputCheck}, {Key: "levels", Input: InputCheck, Default: true}},
	"calendar": {{Key: "ical_url", Input: InputSecret}, {Key: "days", Input: InputNumber, Default: defaultCalDays},
		{Key: "limit", Input: InputNumber, Default: defaultListLimit}, {Key: "hide_all_day", Input: InputCheck},
		sel("color_1", "none", accentColors...), {Key: "ical_url_2", Input: InputSecret}, sel("color_2", "none", accentColors...),
		{Key: "ical_url_3", Input: InputSecret}, sel("color_3", "none", accentColors...)},
	"custom_api": {{Key: "url", Input: InputText, Required: true}, {Key: "fields", Input: InputArea}, {Key: "headers", Input: InputHeaders},
		{Key: "thresholds", Input: InputArea}, {Key: "units", Input: InputArea}},
	"list": {{Key: "entries", Input: InputArea}, sel("columns", "1", "1", "2")},
	"holidays": {{Key: "country", Input: InputText, Default: defaultCountry}, {Key: "state", Input: InputText},
		{Key: "limit", Input: InputNumber, Default: 5}, {Key: "bridges", Input: InputCheck}},
	"xkcd": {{Key: "random", Input: InputCheck}, {Key: "image_only", Input: InputCheck}},
	"apod": {{Key: "api_key", Input: InputSecret}, {Key: "image_only", Input: InputCheck}},
	"joke": {sel("category", "Any", "Any", "Programming", "Misc", "Pun", "Spooky", "Christmas"), sel("lang", "de", "de", "en"),
		{Key: "every", Input: InputNumber}},
	"crypto": {{Key: "coins", Input: InputList, Default: anyList(defaultCoins)}, {Key: "currency", Input: InputText, Default: defaultCurrency},
		{Key: "spark", Input: InputCheck}, {Key: "digits", Input: InputNumber, Default: cryptoDigits}},
	"stocks": {{Key: "tickers", Input: InputList, Default: anyList(defaultTickers)}, sel("change", "day", "day", "week"), {Key: "spark", Input: InputCheck}},
	"flights": {{Key: "airport", Input: InputText, Required: true}, sel("direction", "Departure", "Departure", "Arrival"),
		{Key: "limit", Input: InputNumber, Default: defaultListLimit}, {Key: "api_key", Input: InputSecret}, {Key: "airlines", Input: InputList}},
	"transit": {{Key: "stop", Input: InputText, Required: true}, {Key: "limit", Input: InputNumber, Default: defaultListLimit},
		{Key: "lines", Input: InputList}, {Key: "walk", Input: InputNumber, Default: 0}},
}

// dataModeField lets connection-bound widgets choose live or background data.
var dataModeField = sel(DataModeKey, string(DataAuto), string(DataAuto), string(DataLive), string(DataStored))

// liveCapable are types whose data comes from a connection.
var liveCapable = map[string]bool{"link": true, "kpi": true, "table": true, "chart": true, "progress": true,
	"sysinfo": true, "monitors": true, "hass": true, "glances_chart": true, "kimai_timer": true}

// FieldsOf returns the config fields of a widget type.
func FieldsOf(key string) []Field {
	if liveCapable[key] {
		return append(append([]Field(nil), fieldsByType[key]...), dataModeField)
	}
	return fieldsByType[key]
}

// FormValue is one field with its current value, ready for a form.
type FormValue struct {
	Field
	Name     string // form name, "cfg.url"
	Label    string // catalog suffix of the label, "url" or "info"
	Text     string // value as text
	On       bool   // checkbox state
	Lat, Lon string // place: its coordinates
}

// A place field stores the picked name and its coordinates side by side,
// e.g. place: "Weimar, Thüringen, Deutschland", lat: 50.98, lon: 11.33.
const (
	placeKey = "place"
	latKey   = "lat"
	lonKey   = "lon"
)

func lookup(config map[string]any, dotted string) (any, bool) {
	parts := strings.Split(dotted, ".")
	var cur any = config
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		if cur, ok = m[p]; !ok {
			return nil, false
		}
	}
	return cur, true
}

func textOf(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			parts = append(parts, textOf(item))
		}
		return strings.Join(parts, listSep+" ")
	}
	return ""
}

// FormValues pairs each field of a type with its value in config.
func FormValues(key string, config map[string]any) []FormValue {
	return formValues(FieldsOf(key), config)
}

// FrameFormValues is FormValues for the frame fields.
func FrameFormValues(key string, config map[string]any) []FormValue {
	return formValues(FrameFieldsOf(key), config)
}

func formValues(fields []Field, config map[string]any) []FormValue {
	out := make([]FormValue, 0, len(fields))
	for _, f := range fields {
		v, ok := lookup(config, f.Key)
		if !ok {
			v = f.Default
		}
		label := f.Key
		if i := strings.LastIndex(label, "."); i >= 0 {
			label = label[:i]
		}
		on, _ := v.(bool)
		text := textOf(v)
		switch f.Input {
		case InputSelect:
			if !slices.Contains(f.Options, text) {
				text = textOf(f.Default)
			}
		case InputLinks:
			text = linksText(v)
		case InputHeaders:
			text = headersText(v)
		case InputSecret:
			text = ""
		}
		value := FormValue{Field: f, Name: FormPrefix + f.Key, Label: label, Text: text, On: on}
		if f.Input == InputPlace {
			value.Lat, value.Lon = textOf(config[latKey]), textOf(config[lonKey])
		}
		out = append(out, value)
	}
	return out
}

func set(config map[string]any, dotted string, value any) {
	parts := strings.Split(dotted, ".")
	cur := config
	for _, p := range parts[:len(parts)-1] {
		next, ok := cur[p].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[p] = next
		}
		cur = next
	}
	cur[parts[len(parts)-1]] = value
}

func splitList(raw string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == '\n' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ParseForm turns submitted form values into a config map; get returns
// the value of one form field ("" if missing).
func ParseForm(key string, get func(name string) string) map[string]any {
	config := map[string]any{}
	edited := get(FormPrefix+FormMarker) != ""
	for _, f := range append(FieldsOf(key), FrameFieldsOf(key)...) {
		raw := strings.TrimSpace(get(FormPrefix + f.Key))
		switch f.Input {
		case InputCheck:
			if raw != "" || edited {
				set(config, f.Key, raw != "")
			}
		case InputNumber:
			if n, err := strconv.ParseFloat(strings.ReplaceAll(raw, ",", "."), 64); err == nil {
				set(config, f.Key, n)
			}
		case InputList:
			list := []any{}
			for _, s := range splitList(raw) {
				list = append(list, s)
			}
			set(config, f.Key, list)
		case InputNumbers:
			list := []any{}
			for _, s := range splitList(raw) {
				if n, err := strconv.Atoi(s); err == nil {
					list = append(list, float64(n))
				}
			}
			set(config, f.Key, list)
		case InputLinks:
			set(config, f.Key, parseLinks(raw))
		case InputSecret:
			set(config, f.Key, raw)
		case InputPlace:
			lat, errLat := strconv.ParseFloat(strings.TrimSpace(get(FormPrefix+latKey)), 64)
			lon, errLon := strconv.ParseFloat(strings.TrimSpace(get(FormPrefix+lonKey)), 64)
			if errLat == nil && errLon == nil {
				config[placeKey], config[latKey], config[lonKey] = raw, lat, lon
			}
		case InputHeaders:
			if raw == secretClear {
				set(config, f.Key, raw)
				continue
			}
			set(config, f.Key, parseHeaders(raw))
		default:
			if raw != "" || f.Required {
				set(config, f.Key, raw)
			}
		}
	}
	return config
}

// linksText: [{title: Admin, url: https://x/admin}] → "Admin | https://x/admin".
func linksText(v any) string {
	var lines []string
	for _, l := range subLinks(v) {
		line := l.Title + " " + linkSep + " " + l.URL
		if l.Icon != "" {
			line += " " + linkSep + " " + l.Icon
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func parseLinks(raw string) []any {
	out := []any{}
	for _, line := range strings.Split(raw, "\n") {
		parts := strings.Split(line, linkSep)
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}

		// A bare URL is allowed: "https://x/admin".
		if len(parts) == 1 {
			parts = []string{"", parts[0]}
		}
		if parts[1] == "" {
			continue
		}
		item := map[string]any{"title": parts[0], "url": parts[1]}
		if len(parts) > 2 && parts[2] != "" {
			item["icon"] = parts[2]
		}
		out = append(out, item)
	}
	return out
}

// headersText: {X-Api: a} → "X-Api: a", sorted for a stable form.
func headersText(v any) string {
	m := stringMap(v)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, k+headerSep+" "+m[k])
	}
	return strings.Join(lines, "\n")
}

func parseHeaders(raw string) map[string]any {
	out := map[string]any{}
	for _, line := range strings.Split(raw, "\n") {
		name, value, ok := strings.Cut(line, headerSep)
		if name = strings.TrimSpace(name); !ok || name == "" {
			continue
		}
		out[name] = strings.TrimSpace(value)
	}
	return out
}
