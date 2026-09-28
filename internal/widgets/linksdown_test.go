package widgets

import (
	"testing"
	"time"
)

// Longest outages come first; links down only since today come last.
func TestLinksDownOrder(t *testing.T) {
	links := []DownLink{{Title: "today"}, {Title: "old", Since: time.Now().AddDate(0, 0, -5)}, {Title: "newer", Since: time.Now().AddDate(0, 0, -1)}}
	v := linksDownView(LinksDownConfig{Limit: 2}, map[string]any{LinksDownSlot: links}, ViewCtx{})
	got := v["Links"].([]DownLink)
	if len(got) != 2 || got[0].Title != "old" || got[1].Title != "newer" || v["More"] != 1 {
		t.Fatalf("view: %+v", v)
	}
}
