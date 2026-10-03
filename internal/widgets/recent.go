package widgets

// "timeline_recent": the last things that happened (updates, redeployed
// stacks, hints that came or went), newest first, for "what happened
// while I was away".

import "time"

// TimelineSlot carries []TimelineItem for ExtraTimeline.
const TimelineSlot = "timeline"

// kindUpdate is a version change; other events (a recorded change, a
// restore test) are no hints either.
const kindUpdate = "update"

// hintKinds are the timeline entries of hints.
var hintKinds = map[string]bool{"opened": true, "resolved": true, "reopened": true}

// isHintKind tells hint entries (opened, resolved, reopened) from updates
// and other events.
func isHintKind(kind string) bool { return hintKinds[kind] }

// TimelineDays is how far back the tile looks.
const TimelineDays = 7

// TimelineItem is one timeline line.
type TimelineItem struct {
	At                    time.Time
	Kind, Subject, Detail string // Kind: update, opened, resolved, reopened
	HintID                int64
	Count                 int // > 1: a burst of this many hints
	// Cause is the update shortly before a new hint ("Immich v1 → v2"),
	// CauseMin how many minutes before; "" = none.
	Cause    string
	CauseMin int
}

// RecentConfig is the "timeline_recent" widget's config.
type RecentConfig struct {
	Limit int
	Days  int
	Kinds string // "" all, "updates" or "hints"
}

const defaultRecent = 6

// recentAll is the kinds select's "everything", stored as "" in RecentConfig.
const recentAll = "all"

func decodeRecent(r Raw) RecentConfig {
	kinds := r.Pick("kinds")
	if kinds == recentAll {
		kinds = ""
	}
	return RecentConfig{Limit: r.Int("limit"), Days: r.Int("days"), Kinds: kinds}
}

// ExtraDays is how far back the widgets service loads the timeline.
func (c RecentConfig) ExtraDays() int { return c.Days }

func recentView(cfg RecentConfig, results map[string]any, _ ViewCtx) map[string]any {
	all, _ := results[TimelineSlot].([]TimelineItem)
	var items []TimelineItem
	for _, it := range all {
		// Bursts of hints have no single HintID: tell kinds by Kind.
		if cfg.Kinds == "" || (cfg.Kinds == "hints") == isHintKind(it.Kind) {
			items = append(items, it)
		}
	}
	return map[string]any{"Items": items[:min(len(items), cfg.Limit)], "More": max(len(items)-cfg.Limit, 0)}
}

func init() {
	Tile[RecentConfig]{Key: "timeline_recent", Detail: recentDetail, Category: CategoryInsight, Topic: TopicOverview, RefreshS: 900, Extra: ExtraTimeline,
		Fields: []Field{{Key: "limit", Input: InputNumber, Default: defaultRecent, Min: "1", Max: "30"}, {Key: "days", Input: InputNumber, Default: TimelineDays, Min: "1", Max: "90"},
			sel("kinds", recentAll, recentAll, "updates", "hints")},
		Decode: decodeRecent, View: recentView}.add()
}
