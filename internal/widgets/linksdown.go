package widgets

// "links_down": every link tile of the space whose status check fails
// now, longest down first ("Plex · seit 25.09."), so red links spread
// over a board are seen in one place.

import (
	"sort"
	"time"
)

// LinksDownSlot carries []DownLink for ExtraLinksDown.
const LinksDownSlot = "links_down"

// DownLink is a link tile whose status check fails.
type DownLink struct {
	Title, URL string
	Since      time.Time // first day without any answer; zero = answered earlier today
}

// LinksDownConfig is the "links_down" widget's config.
type LinksDownConfig struct{ Limit int }

const defaultLinksDown = 8

func decodeLinksDown(raw map[string]any) any {
	return LinksDownConfig{Limit: clampInt(asInt(raw["limit"], defaultLinksDown), 1, 50)}
}

func linksDownView(cfgAny any, results map[string]any, _ ViewCtx) map[string]any {
	cfg := cfgAny.(LinksDownConfig)
	links, _ := results[LinksDownSlot].([]DownLink)
	sort.SliceStable(links, func(i, j int) bool { return downSince(links[i]).Before(downSince(links[j])) })
	return map[string]any{"Links": links[:min(len(links), cfg.Limit)], "More": max(len(links)-cfg.Limit, 0), "Total": len(links)}
}

// downSince orders "since today" after every dated outage.
func downSince(l DownLink) time.Time {
	if l.Since.IsZero() {
		return time.Now()
	}
	return l.Since
}

func init() {
	Register(WidgetType{Key: "links_down", Decode: decodeLinksDown, Template: "widgets/links_down", Category: CategoryStart,
		RefreshS: 300, View: linksDownView, Extra: ExtraLinksDown})
}
