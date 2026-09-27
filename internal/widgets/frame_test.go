package widgets_test

import (
	"testing"

	"andon/internal/widgets"
)

// TestFrame: every tile but links gets the frame fields; the form round
// trip keeps them apart from the type's own config, and FrameOf reads
// them with safe defaults.
func TestFrame(t *testing.T) {
	if len(widgets.FrameFieldsOf("link")) != 0 {
		t.Fatal("links draw no card frame")
	}
	keys := map[string]bool{}
	for _, f := range widgets.FrameFieldsOf("deadlines") {
		keys[f.Key] = true
	}
	for _, want := range []string{"frame_accent", "frame_header", "frame_link", "frame_icon", "frame_refresh", "frame_density", "frame_round"} {
		if !keys[want] {
			t.Fatalf("deadlines lacks %s: %v", want, keys)
		}
	}
	for _, f := range widgets.FrameFieldsOf("clock") {
		if f.Key == "frame_only_issues" {
			t.Fatal("a clock always has something to show")
		}
	}
	hasCalm := false
	for _, f := range widgets.FrameFieldsOf("backups") {
		hasCalm = hasCalm || f.Key == "frame_only_issues"
	}
	if !hasCalm {
		t.Fatal("backups can hide when all is well")
	}

	form := map[string]string{"cfg.frame_accent": "green", "cfg.frame_header": "off", "cfg.frame_refresh": "60",
		"cfg.frame_round": "thousand", "cfg.frame_only_issues": "on", "cfg.max_hours": "30"}
	cfg := widgets.ParseForm("backups", func(name string) string { return form[name] })
	f := widgets.FrameOf(cfg)
	if f.Accent != "green" || f.Header != widgets.HeaderOff || f.RefreshS != 60 || f.Round != widgets.RoundThousand || !f.OnlyIssues {
		t.Fatalf("frame: %+v", f)
	}
	if cfg["max_hours"] != 30.0 {
		t.Fatalf("own config lost: %v", cfg)
	}
	if d := widgets.FrameOf(map[string]any{"frame_accent": "pink", "frame_refresh": "7"}); d.Accent != "" || d.RefreshS != 0 || d.Header != widgets.HeaderNormal {
		t.Fatalf("unknown values not ignored: %+v", d)
	}
}

// TestIsCalm: views that report nothing to do, and ones that do.
func TestIsCalm(t *testing.T) {
	if !widgets.IsCalm("monitors", map[string]any{"Up": 3, "Total": 3}) || widgets.IsCalm("monitors", map[string]any{"Up": 2, "Total": 3}) {
		t.Fatal("monitors")
	}
	if !widgets.IsCalm("hints", map[string]any{"Hints": []int{}}) || widgets.IsCalm("hints", map[string]any{"Hints": []int{1}}) {
		t.Fatal("hints")
	}
	if widgets.IsCalm("clock", map[string]any{}) {
		t.Fatal("a clock is never calm")
	}
}
