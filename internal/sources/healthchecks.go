package sources

// Healthchecks (healthchecks.io or self-hosted): cron jobs and other
// scheduled work ping a check; the check goes down when a ping is late.
// A read-only API key is enough.
//
//	GET api/v3/checks/   X-Api-Key   → {"checks": [{name, tags, status, last_ping, next_ping, grace, …}]}

import (
	"context"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

const hcChecks = "api/v3/checks/"

// HeartbeatState is a check's status as Healthchecks names it.
type HeartbeatState string

const (
	HeartbeatUp      HeartbeatState = "up"
	HeartbeatDown    HeartbeatState = "down"
	HeartbeatGrace   HeartbeatState = "grace"   // late, not down yet
	HeartbeatPaused  HeartbeatState = "paused"  // switched off on purpose
	HeartbeatNew     HeartbeatState = "new"     // never pinged
	HeartbeatStarted HeartbeatState = "started" // a run is going on
)

// Heartbeat is one check.
type Heartbeat struct {
	Name     string
	Tags     []string
	Status   HeartbeatState
	LastPing time.Time // zero = never
	NextPing time.Time // zero = unknown (cron checks) or paused
	Grace    int       // seconds a ping may be late
	Schedule string    // cron expression, "" for period checks
	Timeout  int       // seconds between pings of a period check
	Pings    int
}

// HealthchecksDataset is every check the key sees.
type HealthchecksDataset struct {
	URL    string
	Checks []Heartbeat
}

var HealthchecksData = source{key: "healthchecks.data", ttl: dataTTL, service: enums.ServiceHealthchecks, fetch: fetchHealthchecks}

func fetchHealthchecks(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoHealthchecks(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.HeaderApi(sctx.URL, "X-Api-Key", secret, sctx.TLS())
	raw, err := api.Get(ctx, hcChecks, nil)
	if err != nil {
		return nil, fetchError(err)
	}
	data := &HealthchecksDataset{URL: sctx.URL}
	for _, item := range asList(asMap(raw)["checks"]) {
		data.Checks = append(data.Checks, heartbeatOf(asMap(item)))
	}
	return data, nil
}

func heartbeatOf(c map[string]any) Heartbeat {
	return Heartbeat{Name: asStr(c["name"]), Tags: strings.Fields(asStr(c["tags"])), Status: HeartbeatState(asStr(c["status"])),
		LastPing: parseTime(c["last_ping"]), NextPing: parseTime(c["next_ping"]), Grace: int(asFloat(c["grace"])),
		Schedule: asStr(c["schedule"]), Timeout: int(asFloat(c["timeout"])), Pings: int(asFloat(c["n_pings"]))}
}

// Down lists the checks that are down.
func (d *HealthchecksDataset) Down() []Heartbeat {
	var out []Heartbeat
	for _, c := range d.Checks {
		if c.Status == HeartbeatDown {
			out = append(out, c)
		}
	}
	return out
}

// DemoHealthchecks is Studio Weber's checks.
func DemoHealthchecks(now time.Time) *HealthchecksDataset {
	var p struct {
		URL    string
		Checks []struct {
			Heartbeat
			Tags string // space-separated, as the API sends them
		}
	}
	demoworld.MustDecode("heartbeats", now, &p)
	data := &HealthchecksDataset{URL: p.URL}
	for _, c := range p.Checks {
		h := c.Heartbeat
		h.Tags = strings.Fields(c.Tags)
		data.Checks = append(data.Checks, h)
	}
	return data
}

func init() {
	Register(HealthchecksData)
	Register(testOf{HealthchecksData, func(d any) map[string]any {
		data := d.(*HealthchecksDataset)
		return map[string]any{"checks": len(data.Checks), "down": len(data.Down())}
	}})
}
