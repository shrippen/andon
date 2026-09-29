package widgets

import (
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
	Min, Max string // number bounds as the decoder applies them, "" = none
}

func sel(key string, def string, options ...string) Field {
	return Field{Key: key, Input: InputSelect, Options: options, Default: def}
}

var fieldsByType = map[string][]Field{
	"links_down": {{Key: "limit", Input: InputNumber, Default: defaultLinksDown, Min: "1", Max: "50"}},
	"timeline_recent": {{Key: "limit", Input: InputNumber, Default: defaultRecent, Min: "1", Max: "30"}, {Key: "days", Input: InputNumber, Default: TimelineDays, Min: "1", Max: "90"},
		sel("kinds", "all", "all", "updates", "hints")},
	"hint_noise": {sel("days", "14", "14", "30", "90")},
	"hint_trend": {sel("days", "30", "14", "30", "90")},
	"status_light": {sel("red_from", "critical", "critical", "warn"), sel("yellow_from", "warn", "warn", "info", "off"), {Key: "sources", Input: InputList},
		{Key: "direct", Input: InputCheck}, {Key: "text_green", Input: InputText}, {Key: "text_yellow", Input: InputText}, {Key: "text_red", Input: InputText}},
	"exposure": {{Key: "only_problems", Input: InputCheck}},
	"kpi": {sel("metric", string(MetricRevenueYTD), "hours_today", "hours_week", "hours_month", "utilization", "unbilled",
		"revenue_ytd", "revenue_month", "open_amount", "overdue_amount", "vat_liability", "tax_reserve",
		"asset_value", "assets_ready", "revenue_forecast", "cash_30", "liquidity_30", "effective_rate", "net_worth", "cash", "safe_to_spend"),
		sel("compare", comparePrevYear, comparePrevYear, comparePrevMonth, compareOff),
		{Key: "target_value", Input: InputNumber, Min: "0"}, {Key: "spark", Input: InputCheck, Default: true}, {Key: "free", Input: InputCheck}},
	"table": {sel("table", "open_invoices", "open_invoices", "unbilled", "budgets", "client_shares", "asset_dates", "trips", "effective_rates", "app_usage", "payment_morale",
		"full_rates", "unbilled_aging", "payment_matches", "missing_receipts", "subscriptions", "budget_forecast", "project_margins", "exposure", "domain_chain"),
		{Key: "limit", Input: InputNumber, Default: 8, Min: "1", Max: "50"}, {Key: "hide_cols", Input: InputList},
		sel("sort", sortAsIs, sortAsIs, sortAmountDesc, sortAmountAsc, sortName, sortDate), {Key: "sum_row", Input: InputCheck}},
	"chart": {sel("chart", "revenue", "revenue", "hours", "seasonal"), {Key: "months", Input: InputNumber, Default: 12, Min: "3", Max: "24"},
		{Key: "show_prev", Input: InputCheck, Default: true}, {Key: "values", Input: InputCheck}, {Key: "goal_line", Input: InputCheck}},
	"progress": {{Key: "goal", Input: InputCheck, Default: true}, {Key: "projects", Input: InputList}, {Key: "soll", Input: InputCheck, Default: true},
		{Key: "warn_ahead", Input: InputNumber, Default: 10, Min: "1", Max: "100"}},
	"deadlines": {{Key: "days", Input: InputNumber, Default: 45, Min: "7", Max: "400"}, {Key: "show_vat", Input: InputCheck, Default: true},
		{Key: "show_prepayment", Input: InputCheck, Default: true}, {Key: "show_annual", Input: InputCheck, Default: true},
		{Key: "amounts", Input: InputCheck, Default: true}},
	"trend": {sel("metric", string(TrendOpenAmount), "revenue_ytd", "open_amount", "month_min"), {Key: "days", Input: InputNumber, Default: 90, Min: "7", Max: "730"},
		{Key: "target_value", Input: InputNumber, Min: "0"}, {Key: "smooth", Input: InputCheck}},
	"expiries": {{Key: "days", Input: InputNumber, Default: 90, Min: "7", Max: "400"}, {Key: "limit", Input: InputNumber, Default: 15, Min: "1", Max: "50"}, {Key: "sources", Input: InputList}},
	"updates":  {{Key: "limit", Input: InputNumber, Default: 20, Min: "1", Max: "50"}, {Key: "sources", Input: InputList}, sel("sort", "urgency", "urgency", "age")},
	"update_window": {{Key: "window", Input: InputText}, {Key: "timezone", Input: InputText, Default: defaultTimezone},
		{Key: "use_backup", Input: InputCheck, Default: true}, {Key: "use_streams", Input: InputCheck, Default: true},
		{Key: "use_timer", Input: InputCheck, Default: true}, {Key: "use_meetings", Input: InputCheck, Default: true},
		{Key: "use_price", Input: InputCheck, Default: true}},
	"homelab_cost": {sel("period", "month", "month", "year"), {Key: "power_split", Input: InputCheck, Default: true}},
	"week_story": {sel("period", "days7", "days7", "calendar"), {Key: "show_hours", Input: InputCheck, Default: true},
		{Key: "show_money", Input: InputCheck, Default: true}, {Key: "show_storage", Input: InputCheck, Default: true},
		{Key: "show_power", Input: InputCheck, Default: true}, {Key: "show_hints", Input: InputCheck, Default: true}},
	"storage_forecast": {{Key: "filter", Input: InputList}, {Key: "ahead", Input: InputNumber, Default: storageAhead, Min: "1", Max: "365"}},
	"backups": {{Key: "max_hours", Input: InputNumber, Default: defaultBackupHours, Min: "1", Max: "336"}, {Key: "tools", Input: InputList},
		{Key: "only_problems", Input: InputCheck}, sel("days", "14", "7", "14", "30")},
	"hints": {{Key: "sources", Input: InputList}, sel("min_severity", severityChoices[0], severityChoices...), {Key: "limit", Input: InputNumber, Default: 8, Min: "1", Max: "50"},
		{Key: "show_buttons", Input: InputCheck}, sel("sort", hintSortUrgency, hintSortUrgency, HintSortValue, HintSortAge),
		{Key: "show_levels", Input: InputCheck, Default: true}},
}

// dataModeField lets connection-bound widgets choose live or background data.
var dataModeField = sel(DataModeKey, string(DataAuto), string(DataAuto), string(DataLive), string(DataStored))

// liveCapable are types whose data comes from a connection.
var liveCapable = map[string]bool{
	"kpi":      true,
	"table":    true,
	"chart":    true,
	"progress": true,
}

// FieldsOf returns the config fields of a widget type.
func FieldsOf(key string) []Field {
	kind := registry[key]
	fields, choice := kind.Fields, kind.DataChoice
	if fields == nil {
		fields = fieldsByType[key]
	}
	if choice || liveCapable[key] {
		return append(append([]Field(nil), fields...), dataModeField)
	}
	return fields
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
	config, _ = Upgrade(key, config)
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
