// Package widgets is the widget type contract and registry.
//
// A widget type declares what it needs, never how to get it:
//
//	Register(WidgetType{Key: "rss",
//	    Queries: func(cfg any) []Query { ... }})
//
// The widgets service runs the queries (with access checks and caching)
// and hands the results to the template. Config validation happens per
// type via Decode: known fields only, sane zero-value defaults.
package widgets

import (
	"sort"

	"andon/internal/enums"
)

// ConnUse says whether (and how) a query needs the widget's connection.
type ConnUse string

const (
	// ConnNone is "" (Go's zero value for a string type), not "none": a
	// Query built without setting Conn must default to "no connection
	// needed".
	ConnNone   ConnUse = ""
	ConnWidget ConnUse = "widget"
	ConnInfo   ConnUse = "info"
	// ConnPeer is another connection of the widget's space, found by
	// Query.Service (e.g. the Kimai hours next to Invoice Ninja revenue).
	ConnPeer ConnUse = "peer"
)

// Extra is additional data the widgets service supplies besides the queries.
type Extra string

const (
	ExtraNone   Extra = "none"
	ExtraHints  Extra = "hints"
	ExtraPoints Extra = "points"
	// ExtraHistory hands the space's recorded history to the view as
	// results["history"] (*metrics.History).
	ExtraHistory Extra = "history"
	// ExtraStory hands the caller's week in numbers to the view as
	// results["story"] ([]metrics.StoryLine).
	ExtraStory Extra = "story"
	// ExtraGreeting hands the viewer's name, open hints and the changes
	// since yesterday evening to the view as results["greeting"]
	// (*GreetingData).
	ExtraGreeting Extra = "greeting"
	// ExtraConnHealth hands the viewer's connection strips to the view as
	// results["connhealth"] ([]ConnStrip).
	ExtraConnHealth Extra = "connhealth"
	// ExtraHintBriefs hands the open hints the config picks (HintSource)
	// to the view as results["hints"] ([]HintBrief).
	ExtraHintBriefs Extra = "hint_briefs"
	// ExtraNoise hands the caller's hint traffic to the view as
	// results["noise"] (NoiseData).
	ExtraNoise Extra = "noise"
	// ExtraTimeline hands the caller's recent timeline to the view as
	// results["timeline"] ([]TimelineItem).
	ExtraTimeline Extra = "timeline"
	// ExtraLinksDown hands the space's link tiles whose status check fails
	// to the view as results["links_down"] ([]DownLink).
	ExtraLinksDown Extra = "links_down"
	// ExtraForwarded hands the UIDs of the mailbox's mails already sent to
	// Paperless to the view as results["forwarded"] (map[uint32]bool).
	ExtraForwarded Extra = "forwarded"
	// ExtraCloseTicks hands the viewer's month-close steps ticked by hand
	// to the view as results["close_ticks"] (map[string][]string).
	ExtraCloseTicks Extra = "close_ticks"
	// ExtraKimaiFavs hands the viewer's pinned Kimai pairs of the tile's
	// connection to the view as results["kimai_favs"] ([]KimaiFav).
	ExtraKimaiFavs Extra = "kimai_favs"
	// ExtraIPWatch keeps the viewer's last public IP and hands it to the
	// view as results["ip_seen"] (IPSeen).
	ExtraIPWatch Extra = "ip_watch"
)

// Category groups widget types for the library UI.
type Category string

const (
	CategoryStart   Category = "start"
	CategoryInsight Category = "insight"
)

// ViewCtx is what a view function may use: no I/O, only values.
type ViewCtx struct {
	Today       string // ISO date
	Settings    map[string]any
	Options     map[string]any
	Service     string                    // "" if the widget has no connection
	PeerOptions map[string]map[string]any // options of ConnPeer connections by query name
}

// Query is one data request a widget type needs; the widgets service runs
// it and passes the result back keyed by Name.
type Query struct {
	Name    string
	Source  string
	Params  map[string]any
	Conn    ConnUse
	Service enums.ServiceType // ConnPeer only
}

// DecodeFunc parses a widget's raw JSON config into its typed config value.
type DecodeFunc func(raw map[string]any) any

// QueriesFunc returns the queries a widget instance needs, given its config.
type QueriesFunc func(cfg any) []Query

// ViewFunc shapes query results into template data.
type ViewFunc func(cfg any, results map[string]any, ctx ViewCtx) map[string]any

// WidgetType is one kind of widget (link, rss, clock, kpi, ...).
type WidgetType struct {
	Key      string
	Decode   DecodeFunc
	Template string // defaults to "widgets/<Key>"
	Category Category
	Service  enums.ServiceType // "" if not tied to one service
	RefreshS int               // 0 = no periodic refresh
	// Live: by default the widget's own connection data is fetched on
	// view (short source TTL) instead of read from the background run.
	Live bool
	// Inline widgets render with the page (search needs link tiles in the HTML).
	Inline  bool
	Queries QueriesFunc
	View    ViewFunc
	Detail  DetailFunc // nil: no detail dialog (see detail.go)
	Extra   Extra

	Topic      Topic
	DataChoice bool // the user may pick live or background data
	Fields     []Field
	Renames    []rename
	Check      func(raw map[string]any) string
	Calm       func(view map[string]any) bool

	tile bool // declared as a Tile
}

var registry = map[string]WidgetType{}

// DataMode chooses where a widget's connection data comes from.
type DataMode string

const (
	DataAuto   DataMode = "data_auto"   // the type's default
	DataLive   DataMode = "data_live"   // fetched on view, cached for the source TTL
	DataStored DataMode = "data_stored" // last background run only
)

// DataModeKey is the config key of the data mode.
const DataModeKey = "data_mode"

// LiveData resolves a widget's data mode to live or not.
func LiveData(kind WidgetType, config map[string]any) bool {
	switch DataMode(asString(config[DataModeKey])) {
	case DataLive:
		return true
	case DataStored:
		return false
	}
	return kind.Live
}

// Register adds a widget type to the process-wide registry.
func Register(kind WidgetType) WidgetType {
	if _, taken := registry[kind.Key]; taken {
		panic("widgets: duplicate widget key " + kind.Key)
	}
	if kind.Queries == nil {
		kind.Queries = func(any) []Query { return nil }
	}
	if kind.Template == "" {
		kind.Template = "widgets/" + kind.Key
	}
	registry[kind.Key] = kind
	return kind
}

// Get looks up a widget type by key, or ok=false if unknown.
func Get(key string) (WidgetType, bool) {
	k, ok := registry[key]
	return k, ok
}

// AllTypes returns every registered widget type, grouped by category then key.
func AllTypes() []WidgetType {
	out := make([]WidgetType, 0, len(registry))
	for _, k := range registry {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Key < out[j].Key
	})
	return out
}

// Decode parses a widget's stored config through its type's decoder.
// Returns (nil, false) for an unknown widget type.
func Decode(key string, config map[string]any) (any, bool) {
	kind, ok := registry[key]
	if !ok {
		return nil, false
	}
	if config == nil {
		config = map[string]any{}
	}
	config, _ = Upgrade(key, config)
	return kind.Decode(config), true
}
