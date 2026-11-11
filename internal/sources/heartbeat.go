package sources

// Heartbeats across tools: a dataset that knows scheduled jobs names
// them in one shape, so the heartbeat tile, its rules and the host view
// read every tool the same way, also ones added later. Several
// connections of a tool merge (Merger); several tools sit side by side:
//
//	HealthchecksDataset, … ─► Heartbeats() ─► HeartbeatsOf(datasets)

import (
	"slices"
	"time"

	"andon/internal/enums"
)

// HeartbeatState is a job's status as the tool names it.
type HeartbeatState string

const (
	HeartbeatUp      HeartbeatState = "up"
	HeartbeatDown    HeartbeatState = "down"
	HeartbeatGrace   HeartbeatState = "grace"   // late, not down yet
	HeartbeatPaused  HeartbeatState = "paused"  // switched off on purpose
	HeartbeatNew     HeartbeatState = "new"     // never pinged
	HeartbeatStarted HeartbeatState = "started" // a run is going on
)

// HeartbeatReason says why a job is down or late. The zero value is
// "no reason" (the job is fine), not a guess.
type HeartbeatReason string

const (
	HeartbeatNoReason HeartbeatReason = ""
	HeartbeatFailed   HeartbeatReason = "failed" // the job reported a failure
	HeartbeatMissed   HeartbeatReason = "missed" // the ping stayed away
)

// HeartbeatRun is one run of a job, for its history.
type HeartbeatRun struct {
	Status HeartbeatState // up, down, started; paused for a run that did not count
	At     time.Time      // when it began
	Link   string         // the run's page, "" none
}

// Heartbeat is one watched job.
type Heartbeat struct {
	Name     string
	Tags     []string
	Status   HeartbeatState
	Reason   HeartbeatReason
	Summary  string         // the tool's own words on it, "" none
	Link     string         // the job's page in the tool, "" none
	RunLink  string         // the last run's page, "" none
	Tail     string         // end of the last run's output, "" none
	History  []HeartbeatRun // the last runs, oldest first
	Tool     string         // service key, e.g. "healthchecks"
	ConnID   int64          // the connection it comes from, 0 unknown
	LastPing time.Time      // zero = never
	NextPing time.Time      // zero = unknown (cron checks) or paused
	Grace    int            // seconds a ping may be late
	Schedule string         // cron expression, "" for period checks
	Timeout  int            // seconds between pings of a period check
	Pings    int
}

// HeartbeatSource is a dataset that reports heartbeats.
type HeartbeatSource interface {
	Heartbeats() []Heartbeat
}

// heartbeatServices are the services whose dataset is a HeartbeatSource.
var heartbeatServices []enums.ServiceType

// registerHeartbeat marks a service's dataset as a HeartbeatSource.
func registerHeartbeat(s enums.ServiceType) {
	if !slices.Contains(heartbeatServices, s) {
		heartbeatServices = append(heartbeatServices, s)
	}
}

// HeartbeatServices lists the services that report heartbeats, for the
// tile's queries.
func HeartbeatServices() []enums.ServiceType { return slices.Clone(heartbeatServices) }

// HeartbeatsOf collects the heartbeats of every source among datasets
// (by service key), ordered by key so the result is stable.
func HeartbeatsOf(datasets map[string]any) []Heartbeat {
	keys := make([]string, 0, len(datasets))
	for k, raw := range datasets {
		if _, ok := raw.(HeartbeatSource); ok {
			keys = append(keys, k)
		}
	}
	slices.Sort(keys)

	var out []Heartbeat
	for _, k := range keys {
		out = append(out, datasets[k].(HeartbeatSource).Heartbeats()...)
	}
	return out
}

// DownBeats keeps the jobs that are down.
func DownBeats(all []Heartbeat) []Heartbeat {
	var out []Heartbeat
	for _, h := range all {
		if h.Status == HeartbeatDown {
			out = append(out, h)
		}
	}
	return out
}
