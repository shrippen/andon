package sources

// Jidoka (the sibling service for cron scripts and watches): every job
// and watch becomes a Heartbeat, so the heartbeat tile and its rules read
// it like Healthchecks. Two requests per fetch, however many jobs:
//
//	GET api/v1/status   Bearer → {version, commit, invalid, channels: [{channel, down, since, last_ok}], …}
//	GET api/v1/jobs     Bearer → {jobs: [{id, name, state, reason, summary, last, next, tail, url}], total}
//
// Jidoka pushes a state change to the connection's webhook
// (hooks.Accepts), which drops the cache, so a hint stands at once.

import (
	"context"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

const (
	jidokaStatusPath = "api/v1/status"
	jidokaJobsPath   = "api/v1/jobs"

	// jidokaJobLimit is the most jobs one request lists (Jidoka's page maximum).
	jidokaJobLimit = 500

	// Where a job and a run have their page, below Jidoka's base URL.
	jidokaJobPage = "/jobs/"
	jidokaRunPage = "/runs/"
)

// Jidoka's job states (model.JobState).
const (
	jidokaNever    = "never"
	jidokaRunning  = "running"
	jidokaOK       = "ok"
	jidokaFail     = "fail"
	jidokaTimeout  = "timeout"
	jidokaMissed   = "missed"
	jidokaPaused   = "paused"
	jidokaDisabled = "disabled"
	jidokaInvalid  = "invalid"
)

// jidokaBeat maps a Jidoka state to the heartbeat state and the reason
// it is down; invalid jobs are no heartbeat (they were not loaded).
var jidokaBeat = map[string]struct {
	state  HeartbeatState
	reason HeartbeatReason
}{
	jidokaNever:    {HeartbeatNew, HeartbeatNoReason},
	jidokaRunning:  {HeartbeatStarted, HeartbeatNoReason},
	jidokaOK:       {HeartbeatUp, HeartbeatNoReason},
	jidokaFail:     {HeartbeatDown, HeartbeatFailed},
	jidokaTimeout:  {HeartbeatDown, HeartbeatFailed},
	jidokaMissed:   {HeartbeatDown, HeartbeatMissed},
	jidokaPaused:   {HeartbeatPaused, HeartbeatNoReason},
	jidokaDisabled: {HeartbeatPaused, HeartbeatNoReason},
}

// JidokaChannel is a way Jidoka sends its messages (Apprise, mail).
type JidokaChannel struct {
	Name   string
	Down   bool
	Since  time.Time // zero = not down
	LastOK time.Time // zero = never
}

// JidokaInstance is what one Jidoka connection reports.
type JidokaInstance struct {
	ConnID      int64
	URL         string
	Version     string
	Commit      string
	Invalid     int      // job files Jidoka could not load
	InvalidJobs []string // names of those it still lists
	Channels    []JidokaChannel
	Jobs        []Heartbeat
}

// JidokaDataset is every Jidoka connection of the space, one instance
// each (Merge).
type JidokaDataset struct {
	Instances []JidokaInstance
}

var JidokaData = source{key: "jidoka.data", ttl: dataTTL, service: enums.ServiceJidoka, fetch: fetchJidoka}

func fetchJidoka(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		data := DemoJidoka(time.Now().UTC())
		data.Instances[0].ConnID = sctx.ConnID
		for i := range data.Instances[0].Jobs {
			data.Instances[0].Jobs[i].ConnID = sctx.ConnID
		}
		return data, nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}

	api := services.BearerApi(sctx.URL, secret, sctx.TLS())
	status, err := api.Get(ctx, jidokaStatusPath, nil)
	if err != nil {
		return nil, fetchError(err)
	}
	jobs, err := api.Get(ctx, jidokaJobsPath, url.Values{"limit": {strconv.Itoa(jidokaJobLimit)}})
	if err != nil {
		return nil, fetchError(err)
	}

	in := jidokaStatus(asMap(status))
	in.ConnID, in.URL = sctx.ConnID, sctx.URL
	for _, item := range asList(asMap(jobs)["jobs"]) {
		in.addJob(asMap(item), sctx)
	}
	return &JidokaDataset{Instances: []JidokaInstance{in}}, nil
}

func jidokaStatus(s map[string]any) JidokaInstance {
	in := JidokaInstance{Version: asStr(s["version"]), Commit: asStr(s["commit"]), Invalid: int(asInt64(s["invalid"]))}
	for _, item := range asList(s["channels"]) {
		c := asMap(item)
		in.Channels = append(in.Channels, JidokaChannel{Name: asStr(c["channel"]), Down: asBool(c["down"]),
			Since: parseTime(c["since"]), LastOK: parseTime(c["last_ok"])})
	}
	return in
}

// addJob reads one job of the list: a heartbeat, or, when Jidoka could
// not load its file, only its name.
func (in *JidokaInstance) addJob(j map[string]any, sctx Ctx) {
	state := asStr(j["state"])
	beat, ok := jidokaBeat[state]
	if !ok {
		if state == jidokaInvalid {
			in.InvalidJobs = append(in.InvalidJobs, asStr(j["name"]))
		}
		return
	}

	last := asMap(j["last"])
	b := Heartbeat{Name: asStr(j["name"]), Status: beat.state, Reason: beat.reason, Summary: asStr(j["summary"]),
		Link: asStr(j["url"]), Tool: string(enums.ServiceJidoka), ConnID: sctx.ConnID, Tail: asStr(j["tail"]),
		LastPing: parseTime(cmpTime(last["end"], last["start"])), NextPing: parseTime(j["next"])}
	if id := asInt64(last["id"]); id != 0 {
		b.RunLink = jidokaRunURL(b.Link, id)
	}
	in.Jobs = append(in.Jobs, b)
}

// cmpTime is the first of two API times that is set.
func cmpTime(a, b any) any {
	if asStr(a) != "" {
		return a
	}
	return b
}

// jidokaRunURL is a run's page next to the job's: ".../jobs/backup" →
// ".../runs/42".
func jidokaRunURL(jobURL string, run int64) string {
	i := strings.LastIndex(jobURL, jidokaJobPage)
	if i < 0 {
		return ""
	}
	return jobURL[:i] + jidokaRunPage + strconv.FormatInt(run, 10)
}

func (d *JidokaDataset) Heartbeats() []Heartbeat {
	if d == nil {
		return nil
	}
	var out []Heartbeat
	for _, in := range d.Instances {
		out = append(out, in.Jobs...)
	}
	return out
}

func (d *JidokaDataset) Merge(o any) any {
	x, ok := o.(*JidokaDataset)
	if !ok {
		return d
	}
	return &JidokaDataset{Instances: slices.Concat(d.Instances, x.Instances)}
}

// Down lists the jobs that are down.
func (d *JidokaDataset) Down() []Heartbeat { return DownBeats(d.Heartbeats()) }

// DemoJidoka is Studio Weber's jobs, read from the world's Jidoka section
// the way Jidoka's own demo reads it: a job is down while an alert of
// the world names it, paused while a flag does; its text is the first
// summary it would print.
func DemoJidoka(now time.Time) *JidokaDataset {
	var w struct {
		Settings struct {
			BaseURL  string
			Channels map[string]any
		}
		Jobs []struct {
			ID      string
			Invalid bool
			Config  struct {
				Name string
				Tags []string
			}
			Script struct {
				Output []string
			}
			History struct {
				Summaries []string
				Failures  []struct {
					Since time.Time
					State string
				}
			}
		}
		Flags []struct {
			Job    string
			Paused time.Time
		}
		Alerts []struct {
			Job   string
			Event string
			Since time.Time
		}
		Deliveries []struct {
			At      time.Time
			Channel string
			OK      bool
		}
	}
	demoworld.MustDecode("jobs", now, &w)

	in := JidokaInstance{URL: w.Settings.BaseURL}
	for _, j := range w.Jobs {
		if j.Invalid {
			in.Invalid++
			in.InvalidJobs = append(in.InvalidJobs, j.Config.Name)
			continue
		}

		b := Heartbeat{Name: j.Config.Name, Tags: j.Config.Tags, Status: HeartbeatUp, Tool: string(enums.ServiceJidoka),
			Link: strings.TrimRight(w.Settings.BaseURL, "/") + jidokaJobPage + j.ID}
		if len(j.History.Summaries) > 0 {
			b.Summary = j.History.Summaries[0]
		}
		for _, f := range w.Flags {
			if f.Job == j.ID && !f.Paused.IsZero() {
				b.Status = HeartbeatPaused
			}
		}
		for _, a := range w.Alerts {
			beat, ok := jidokaBeat[a.Event]
			if a.Job != j.ID || !ok {
				continue
			}
			b.Status, b.Reason, b.LastPing = beat.state, beat.reason, a.Since
			for _, f := range j.History.Failures {
				if !f.Since.IsZero() {
					b.LastPing = f.Since
				}
			}
			if len(j.Script.Output) > 0 {
				b.Tail = strings.Join(j.Script.Output, "\n")
				b.Summary = j.Script.Output[len(j.Script.Output)-1]
			}
		}
		in.Jobs = append(in.Jobs, b)
	}

	// A channel is down while its latest delivery failed.
	for name := range w.Settings.Channels {
		c := JidokaChannel{Name: name}
		var latest time.Time
		for _, d := range w.Deliveries {
			if strings.SplitN(d.Channel, ":", 2)[0] != name || d.At.Before(latest) {
				continue
			}
			latest, c.Down = d.At, !d.OK
		}
		in.Channels = append(in.Channels, c)
	}
	slices.SortFunc(in.Channels, func(a, b JidokaChannel) int { return strings.Compare(a.Name, b.Name) })
	return &JidokaDataset{Instances: []JidokaInstance{in}}
}

func init() {
	registerHeartbeat(enums.ServiceJidoka)
	Register(JidokaData)
	Register(testOf{JidokaData, func(d any) map[string]any {
		data := d.(*JidokaDataset)
		return map[string]any{"jobs": len(data.Heartbeats()), "down": len(data.Down())}
	}})
}
