package widgets

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// TestHeartbeatsView: paused checks count neither way; problems come
// first and alone with only_problems; a tag filter keeps its checks.
func TestHeartbeatsView(t *testing.T) {
	data := sources.DemoHealthchecks(time.Now())
	v := heartbeatsView(HeartbeatsConfig{OnlyProblems: true}, data, ViewCtx{})
	lines := v["Lines"].([]HeartbeatLine)
	if v["Total"] != 5 || v["Up"] != 3 || len(lines) != 2 || lines[0].Status != sources.HeartbeatDown || lines[0].Pill != "failed" {
		t.Fatalf("view %+v", v)
	}
	if v := heartbeatsView(HeartbeatsConfig{Tags: []string{"tls"}}, data, ViewCtx{}); v["Total"] != 1 {
		t.Fatalf("tag filter %+v", v)
	}
	if got := durationText(86400) + "|" + durationText(7200) + "|" + durationText(900); got != "1 d|2 h|15 min" {
		t.Fatal(got)
	}
}
