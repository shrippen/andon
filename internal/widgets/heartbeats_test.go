package widgets

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// TestHeartbeatsView: paused checks count neither way; problems come
// first and alone with only_problems; a tag filter keeps its checks.
func TestHeartbeatsView(t *testing.T) {
	data := map[string]any{"healthchecks": sources.DemoHealthchecks(time.Now())}
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

// TestHeartbeatsDetailJidoka: a failed Jidoka job links its page and its
// run and shows the end of its output; a healthy one adds no output.
func TestHeartbeatsDetailJidoka(t *testing.T) {
	data := map[string]any{"jidoka": &sources.JidokaDataset{Instances: []sources.JidokaInstance{{Jobs: []sources.Heartbeat{
		{Name: "bad", Status: sources.HeartbeatDown, Reason: sources.HeartbeatFailed, Tail: "HTTP 502", Link: "https://j/jobs/bad", RunLink: "https://j/runs/4"},
		{Name: "fine", Status: sources.HeartbeatUp, Tail: "done", Link: "https://j/jobs/fine"}}}}}}

	body := heartbeatsDetail(HeartbeatsConfig{}, data, ViewCtx{}).Body.(*DetailBody)
	table := body.Blocks[0].Data.(Table)
	if table.Rows[0][0].Href != "https://j/jobs/bad" || table.Rows[0][2].Href != "https://j/runs/4" {
		t.Fatalf("links %+v", table.Rows[0])
	}
	if len(body.Blocks) != 2 || body.Blocks[1].Kind != BlockCode || body.Blocks[1].Data != "HTTP 502" {
		t.Fatalf("blocks %+v", body.Blocks)
	}
}

// TestRunStrips: a job's runs become bars, oldest first, each with a
// tooltip and its run page; jobs without history draw nothing.
func TestRunStrips(t *testing.T) {
	at := time.Date(2026, 11, 2, 1, 5, 0, 0, time.UTC)
	got := runStrips([]sources.Heartbeat{
		{Name: "a", Status: sources.HeartbeatDown, History: []sources.HeartbeatRun{
			{Status: sources.HeartbeatUp, At: at, Link: "https://j/runs/1"}, {Status: sources.HeartbeatDown, At: at}, {Status: sources.HeartbeatPaused, At: at}}},
		{Name: "b"}})
	if len(got) != 1 || len(got[0].States) != 3 || got[0].States[0] != "ok" || got[0].States[1] != "bad" || got[0].States[2] != "off" ||
		got[0].Titles[0] != "2026-11-02 01:05 UTC" || got[0].Links[0] != "https://j/runs/1" {
		t.Fatalf("strips %+v", got)
	}
}
