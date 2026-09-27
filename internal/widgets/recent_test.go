package widgets_test

import (
	"testing"
	"time"

	"andon/internal/widgets"
)

// TestRecentLimit: the newest entries up to the limit, the rest counted.
func TestRecentLimit(t *testing.T) {
	kind, ok := widgets.Get("timeline_recent")
	if !ok {
		t.Fatal("timeline_recent not registered")
	}
	cfg, _ := widgets.Decode("timeline_recent", map[string]any{"limit": 2.0})
	items := []widgets.TimelineItem{{At: time.Now(), Kind: "update", Subject: "authentik"}, {Kind: "opened"}, {Kind: "resolved"}}
	view := kind.View(cfg, map[string]any{widgets.TimelineSlot: items}, ctxFor("", nil))
	if len(view["Items"].([]widgets.TimelineItem)) != 2 || view["More"] != 1 {
		t.Fatalf("view: %+v", view)
	}
}
