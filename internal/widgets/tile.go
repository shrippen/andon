package widgets

// Tile declares a widget type in one place, with a typed config C:
//
//	Tile[RssConfig]{Key: "rss", Topic: TopicMedia, RefreshS: 30 * 60,
//	    Fields: []Field{{Key: "url", Input: InputText, Required: true},
//	        {Key: "limit", Input: InputNumber, Default: 8, Min: "1", Max: "50"}},
//	    Decode: func(r Raw) RssConfig { return RssConfig{URL: r.URL("url"), Limit: r.Int("limit")} },
//	    Queries: func(cfg RssConfig) []Query { … },
//	    View:    func(cfg RssConfig, results map[string]any, ctx ViewCtx) map[string]any { … },
//	}.add()
//
// add turns it into the untyped WidgetType the widgets service runs:
//
//	stored config ─Upgrade─► Raw ─Decode─► C ─Queries─► results ─View─► template
//	                          ▲ defaults and bounds from Fields

import (
	"strconv"

	"andon/internal/enums"
)

// Tile is one widget type with config C.
type Tile[C any] struct {
	Key      string
	Template string // defaults to "widgets/<Key>"
	Category Category
	Topic    Topic // gallery group; TopicOverview when empty
	Service  enums.ServiceType
	RefreshS int
	// Live: the connection data is fetched on view by default instead of
	// read from the background run; DataChoice lets the user pick.
	Live       bool
	DataChoice bool
	Inline     bool
	Width      Width
	Extra      Extra
	Fields     []Field
	Renames    []rename
	Check      func(raw map[string]any) string // a value the type cannot use, as catalog key
	Calm       func(view map[string]any) bool  // nothing to do (frame option only_issues)
	Decode     func(r Raw) C                   // nil: the zero C
	Queries    func(cfg C) []Query
	View       func(cfg C, results map[string]any, ctx ViewCtx) map[string]any
	Detail     func(cfg C, results map[string]any, ctx ViewCtx) DetailView // the detail dialog, nil = none
	// DetailQueries are fetched only when the dialog opens (cached for the
	// source's TTL); their results join the dialog's.
	DetailQueries func(cfg C) []Query
	// PickQueries read the picked list entry's data when the dialog opens.
	PickQueries func(cfg C, results map[string]any, ctx ViewCtx) []Query
}

// add registers the tile.
func (t Tile[C]) add() {
	kind := WidgetType{Key: t.Key, Template: t.Template, Category: t.Category, Topic: t.Topic,
		Service: t.Service, RefreshS: t.RefreshS, Live: t.Live, DataChoice: t.DataChoice,
		Inline: t.Inline, Width: t.Width, Extra: t.Extra, Fields: t.Fields, Renames: t.Renames, Check: t.Check,
		Calm: t.Calm, tile: true}

	fields := t.Fields
	kind.Decode = func(raw map[string]any) any {
		if t.Decode == nil {
			var zero C
			return zero
		}
		return t.Decode(Raw{m: raw, fields: fields})
	}
	if t.Queries != nil {
		kind.Queries = func(cfg any) []Query { return t.Queries(configOf[C](cfg)) }
	}
	if t.View != nil {
		kind.View = func(cfg any, results map[string]any, ctx ViewCtx) map[string]any {
			return t.View(configOf[C](cfg), results, ctx)
		}
	}
	if t.DetailQueries != nil {
		kind.DetailQueries = func(cfg any) []Query { return t.DetailQueries(configOf[C](cfg)) }
	}
	if t.PickQueries != nil {
		kind.PickQueries = func(cfg any, results map[string]any, ctx ViewCtx) []Query {
			return t.PickQueries(configOf[C](cfg), results, ctx)
		}
	}
	if t.Detail != nil {
		kind.Detail = func(cfg any, results map[string]any, ctx ViewCtx) DetailView {
			return t.Detail(configOf[C](cfg), results, ctx)
		}
	}
	Register(kind)
}

// configOf is cfg as C; the zero C for anything else (a nil config).
func configOf[C any](cfg any) C {
	c, _ := cfg.(C)
	return c
}

// dataView hands a view the typed dataset of the "data" query; while it
// is missing (not fetched yet, failed) the tile shows its empty state.
func dataView[C, D any](view func(cfg C, data D, ctx ViewCtx) map[string]any) func(C, map[string]any, ViewCtx) map[string]any {
	return func(cfg C, results map[string]any, ctx ViewCtx) map[string]any {
		data, ok := results[dataName].(D)
		if !ok {
			return map[string]any{}
		}
		return view(cfg, data, ctx)
	}
}

// ownData is the query of a tile that shows its connection's dataset.
func ownData[C any](C) []Query { return dataQuery(nil) }

// Raw reads a stored config through a type's fields: a missing value is
// the field's Default, a number is kept within Min and Max, a select
// holds one of its Options. With {Key: "limit", Default: 8, Min: "1",
// Max: "50"}:
//
//	{}             → r.Int("limit") = 8
//	{"limit": 80}  → r.Int("limit") = 50
type Raw struct {
	m      map[string]any
	fields []Field
}

// Map is the stored config, for values no field describes.
func (r Raw) Map() map[string]any { return r.m }

// Get is the stored value of key, nil when missing.
func (r Raw) Get(key string) any { return r.m[key] }

// Has tells whether key is stored.
func (r Raw) Has(key string) bool {
	_, ok := r.m[key]
	return ok
}

func (r Raw) field(key string) Field {
	for _, f := range r.fields {
		if f.Key == key {
			return f
		}
	}
	return Field{Key: key}
}

// Int is a whole number within the field's bounds.
func (r Raw) Int(key string) int {
	f := r.field(key)
	n := asInt(r.m[key], asInt(f.Default, 0))
	if lo, ok := bound(f.Min); ok {
		n = max(n, int(lo))
	}
	if hi, ok := bound(f.Max); ok {
		n = min(n, int(hi))
	}
	return n
}

// Float is a number within the field's bounds.
func (r Raw) Float(key string) float64 {
	f := r.field(key)
	n, ok := r.m[key].(float64)
	if !ok {
		n = number(f.Default)
	}
	if lo, ok := bound(f.Min); ok {
		n = max(n, lo)
	}
	if hi, ok := bound(f.Max); ok {
		n = min(n, hi)
	}
	return n
}

// Bool is a checkbox, its Default while unset.
func (r Raw) Bool(key string) bool {
	def, _ := r.field(key).Default.(bool)
	return boolOr(r.m[key], def)
}

// String is a text value as stored.
func (r Raw) String(key string) string {
	if s, ok := r.m[key].(string); ok {
		return s
	}
	s, _ := r.field(key).Default.(string)
	return s
}

// Pick is a select's value: one of its Options, else its Default.
func (r Raw) Pick(key string) string {
	f := r.field(key)
	v := asString(r.m[key])
	for _, o := range f.Options {
		if o == v {
			return v
		}
	}
	return textOf(f.Default)
}

// List is a list of strings.
func (r Raw) List(key string) []string { return asStringList(r.m[key]) }

// Lower is a list of strings in lower case, for matching names.
func (r Raw) Lower(key string) []string { return lowerList(r.m[key]) }

// Ints is a list of whole numbers.
func (r Raw) Ints(key string) []int { return asIntList(r.m[key]) }

// URL is a web address; script URLs are dropped (see webURL).
func (r Raw) URL(key string) string { return webURL(r.m[key]) }

// bound parses a field's Min or Max; ok false when unset.
func bound(s string) (float64, bool) {
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}

// number is a Default as float64: 8 and 8.0 alike.
func number(v any) float64 {
	switch n := v.(type) {
	case int:
		return float64(n)
	case float64:
		return n
	}
	return 0
}
