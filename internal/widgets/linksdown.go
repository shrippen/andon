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
	Cause      string    // the check's error ("connection refused: nas.lan") or "HTTP 502"
}

// LinksDownConfig is the "links_down" widget's config.
type LinksDownConfig struct{ Limit int }

const defaultLinksDown = 8

func linksDownView(cfg LinksDownConfig, results map[string]any, _ ViewCtx) map[string]any {
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
	Tile[LinksDownConfig]{Key: "links_down", Detail: linksDownDetail, Category: CategoryStart, Topic: TopicOverview, RefreshS: 300, Extra: ExtraLinksDown,
		Fields: []Field{{Key: "limit", Input: InputNumber, Default: defaultLinksDown, Min: "1", Max: "50"}},
		Decode: func(r Raw) LinksDownConfig { return LinksDownConfig{Limit: r.Int("limit")} },
		View:   linksDownView}.add()
}
