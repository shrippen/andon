package widgets

// "timeline_recent": the last things that happened (updates, redeployed
// stacks, hints that came or went), newest first, for "what happened
// while I was away".

import "time"

// TimelineSlot carries []TimelineItem for ExtraTimeline.
const TimelineSlot = "timeline"

// TimelineDays is how far back the tile looks.
const TimelineDays = 7

// TimelineItem is one timeline line.
type TimelineItem struct {
	At                    time.Time
	Kind, Subject, Detail string // Kind: update, opened, resolved, reopened
	HintID                int64
}

// RecentConfig is the "timeline_recent" widget's config.
type RecentConfig struct {
	Limit int
	Days  int
	Kinds string // "" all, "updates" or "hints"
}

const defaultRecent = 6

func decodeRecent(raw map[string]any) any {
	kinds, _ := raw["kinds"].(string)
	if kinds != "updates" && kinds != "hints" {
		kinds = ""
	}
	return RecentConfig{Limit: clampInt(asInt(raw["limit"], defaultRecent), 1, 30), Days: clampInt(asInt(raw["days"], TimelineDays), 1, 90), Kinds: kinds}
}

// ExtraDays is how far back the widgets service loads the timeline.
func (c RecentConfig) ExtraDays() int { return c.Days }

func recentView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	all, _ := results[TimelineSlot].([]TimelineItem)
	cfg := cfgAny.(RecentConfig)
	var items []TimelineItem
	for _, it := range all {
		if cfg.Kinds == "" || (cfg.Kinds == "hints") == (it.HintID != 0) {
			items = append(items, it)
		}
	}
	return map[string]any{"Items": items[:min(len(items), cfg.Limit)], "More": max(len(items)-cfg.Limit, 0)}
}

func init() {
	Register(WidgetType{Key: "timeline_recent", Decode: decodeRecent, Template: "widgets/timeline_recent", Category: CategoryInsight,
		RefreshS: 900, View: recentView, Extra: ExtraTimeline})
}
