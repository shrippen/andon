package widgets

// "heartbeats": the Healthchecks checks, problems first. The tile counts
// the healthy ones and names those down or late; the dialog lists every
// check with its last ping and the hints, including a check that
// disagrees with the backup it watches (cross.heartbeat_backup).

import (
	"slices"
	"sort"
	"strconv"
	"strings"

	"andon/internal/enums"
	"andon/internal/sources"
)

// HeartbeatsConfig is the "heartbeats" widget's config.
type HeartbeatsConfig struct {
	Tags         []string // only checks with one of these tags, empty = all
	OnlyProblems bool
}

func init() {
	Tile[HeartbeatsConfig]{Key: "heartbeats", Detail: dataDetail(heartbeatsDetail), Category: CategoryInsight, Topic: TopicHomelab,
		Service: enums.ServiceHealthchecks, RefreshS: 300,
		Fields: []Field{{Key: "tags", Input: InputList}, {Key: "only_problems", Input: InputCheck, Default: true}},
		Decode: func(r Raw) HeartbeatsConfig {
			return HeartbeatsConfig{Tags: r.Lower("tags"), OnlyProblems: r.Bool("only_problems")}
		},
		Queries: ownData[HeartbeatsConfig], View: dataView(heartbeatsView),
		Calm: func(v map[string]any) bool { return v["Total"] != nil && v["Total"] != 0 && v["Up"] == v["Total"] }}.add()
}

// heartbeatPill is the Kante pill state of a check's status; paused and
// new ones get the plain pill.
var heartbeatPill = map[sources.HeartbeatState]string{
	sources.HeartbeatUp: "applied", sources.HeartbeatStarted: "applied", sources.HeartbeatDown: "failed",
	sources.HeartbeatGrace: "locked", // Kante's warning pill
}

// heartbeatRank sorts problems first: down, late, never pinged, the rest.
var heartbeatRank = map[sources.HeartbeatState]int{sources.HeartbeatDown: 0, sources.HeartbeatGrace: 1, sources.HeartbeatNew: 2}

// HeartbeatLine is one check on the tile.
type HeartbeatLine struct {
	sources.Heartbeat
	Pill string
}

// counted: paused checks are switched off on purpose and count neither way.
func counted(c sources.Heartbeat) bool { return c.Status != sources.HeartbeatPaused }

// troubled: down, late or never pinged.
func troubled(c sources.Heartbeat) bool {
	_, bad := heartbeatRank[c.Status]
	return bad
}

// taggedChecks keeps the checks with one of the tags (all without tags),
// problems first, then by name.
func taggedChecks(data *sources.HealthchecksDataset, tags []string) []sources.Heartbeat {
	var out []sources.Heartbeat
	for _, c := range data.Checks {
		if len(tags) > 0 && !slices.ContainsFunc(c.Tags, func(t string) bool { return slices.Contains(tags, strings.ToLower(t)) }) {
			continue
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri, okI := heartbeatRank[out[i].Status]
		rj, okJ := heartbeatRank[out[j].Status]
		if okI != okJ {
			return okI
		}
		if ri != rj {
			return ri < rj
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func heartbeatsView(cfg HeartbeatsConfig, data *sources.HealthchecksDataset, _ ViewCtx) map[string]any {
	up, total := 0, 0
	var lines []HeartbeatLine
	for _, c := range taggedChecks(data, cfg.Tags) {
		if counted(c) {
			total++
		}
		if c.Status == sources.HeartbeatUp || c.Status == sources.HeartbeatStarted {
			up++
		}
		if cfg.OnlyProblems && !troubled(c) {
			continue
		}
		lines = append(lines, HeartbeatLine{Heartbeat: c, Pill: heartbeatPill[c.Status]})
	}
	return map[string]any{"Up": up, "Total": total, "Lines": lines}
}

// heartbeatState is the Kante state of a check in the dialog's table.
var heartbeatState = map[sources.HeartbeatState]string{sources.HeartbeatDown: "bad", sources.HeartbeatGrace: "warn"}

func heartbeatsDetail(cfg HeartbeatsConfig, data *sources.HealthchecksDataset, _ ViewCtx, results map[string]any) DetailView {
	counts := map[sources.HeartbeatState]int{}
	var rows [][]Cell
	for _, c := range taggedChecks(data, cfg.Tags) {
		counts[c.Status]++
		rows = append(rows, []Cell{{Value: c.Name}, {Value: Txt("heartbeat.state." + string(c.Status)), State: heartbeatState[c.Status]},
			{Value: agoOf(c.LastPing)}, {Value: heartbeatPlan(c)}, {Value: strings.Join(c.Tags, " ")}})
	}
	body := &DetailBody{
		Facts: []Kpi{{Value: counts[sources.HeartbeatUp] + counts[sources.HeartbeatStarted], Label: T("heartbeat.facts.up"), Tier: "green"},
			{Value: counts[sources.HeartbeatDown], Label: T("heartbeat.facts.down"), Tier: tierIf(counts[sources.HeartbeatDown] > 0, "red", "")},
			{Value: counts[sources.HeartbeatGrace], Label: T("heartbeat.facts.late"), Tier: tierIf(counts[sources.HeartbeatGrace] > 0, "yellow", "")},
			{Value: counts[sources.HeartbeatPaused], Label: T("heartbeat.facts.paused")}},
		Blocks: []Block{{Kind: BlockTable, Label: T("heartbeat.checks"), Data: Table{
			Head: []Text{T("heartbeat.col.name"), T("heartbeat.col.state"), T("heartbeat.col.last"), T("heartbeat.col.plan"), T("heartbeat.col.tags")},
			Rows: rows}}},
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// heartbeatPlan is when a check expects its pings: its cron line, or
// every so many hours.
func heartbeatPlan(c sources.Heartbeat) any {
	if c.Schedule != "" {
		return c.Schedule
	}
	if c.Timeout > 0 {
		return TxtA("heartbeat.every", "time", durationText(c.Timeout))
	}
	return "–"
}

// durationText writes seconds in their largest whole unit: 86400 → "1 d",
// 7200 → "2 h", 900 → "15 min".
func durationText(seconds int) string {
	const minute, hour, day = 60, 3600, 86400
	switch {
	case seconds >= day && seconds%day == 0:
		return strconv.Itoa(seconds/day) + " d"
	case seconds >= hour && seconds%hour == 0:
		return strconv.Itoa(seconds/hour) + " h"
	}
	return strconv.Itoa(seconds/minute) + " min"
}
