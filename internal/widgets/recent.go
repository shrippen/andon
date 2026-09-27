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
type RecentConfig struct{ Limit int }

const defaultRecent = 6

func decodeRecent(raw map[string]any) any {
	return RecentConfig{Limit: clampInt(asInt(raw["limit"], defaultRecent), 1, 30)}
}

func recentView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	items, _ := results[TimelineSlot].([]TimelineItem)
	limit := cfgAny.(RecentConfig).Limit
	return map[string]any{"Items": items[:min(len(items), limit)], "More": max(len(items)-limit, 0)}
}

func init() {
	Register(WidgetType{Key: "timeline_recent", Decode: decodeRecent, Template: "widgets/timeline_recent", Category: CategoryInsight,
		RefreshS: 900, View: recentView, Extra: ExtraTimeline})
}
