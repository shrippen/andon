package sources

// Healthchecks (healthchecks.io or self-hosted): cron jobs and other
// scheduled work ping a check; the check goes down when a ping is late.
// A read-only API key is enough.
//
//	GET api/v3/checks/   X-Api-Key   → {"checks": [{name, tags, status, last_ping, next_ping, grace, …}]}

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

const hcChecks = "api/v3/checks/"

// HealthchecksDataset is every check the key sees.
type HealthchecksDataset struct {
	URL    string
	Checks []Heartbeat
}

var HealthchecksData = source{key: "healthchecks.data", ttl: dataTTL, service: enums.ServiceHealthchecks, fetch: fetchHealthchecks}

// hcDetails is the path of a check's page in the Healthchecks UI.
const hcDetails = "checks/%s/details/"

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
		data.Checks = append(data.Checks, heartbeatOf(asMap(item), sctx.URL))
	}
	return data, nil
}

// heartbeatOf reads a check. The API cannot tell a failure signal from
// a missing ping, so a down or late check counts as missed. The link
// goes to the check's page when the key sees its uuid (read-write keys),
// else to the instance.
func heartbeatOf(c map[string]any, base string) Heartbeat {
	b := Heartbeat{Name: asStr(c["name"]), Tags: strings.Fields(asStr(c["tags"])), Status: HeartbeatState(asStr(c["status"])),
		LastPing: parseTime(c["last_ping"]), NextPing: parseTime(c["next_ping"]), Grace: int(asFloat(c["grace"])),
		Schedule: asStr(c["schedule"]), Timeout: int(asFloat(c["timeout"])), Pings: int(asFloat(c["n_pings"])),
		Tool: string(enums.ServiceHealthchecks), Link: base}
	b.Reason = hcReason(b.Status)
	if uuid := asStr(c["uuid"]); uuid != "" {
		b.Link = strings.TrimRight(base, "/") + "/" + fmt.Sprintf(hcDetails, uuid)
	}
	return b
}

// hcReason: a down or late check missed its ping.
func hcReason(s HeartbeatState) HeartbeatReason {
	if s == HeartbeatDown || s == HeartbeatGrace {
		return HeartbeatMissed
	}
	return HeartbeatNoReason
}

func (d *HealthchecksDataset) Heartbeats() []Heartbeat {
	if d == nil {
		return nil
	}
	return d.Checks
}

func (d *HealthchecksDataset) Merge(o any) any {
	x, ok := o.(*HealthchecksDataset)
	if !ok {
		return d
	}
	return &HealthchecksDataset{URL: d.URL, Checks: slices.Concat(d.Checks, x.Checks)}
}

// Down lists the checks that are down.
func (d *HealthchecksDataset) Down() []Heartbeat { return DownBeats(d.Checks) }

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
		h.Tool = string(enums.ServiceHealthchecks)
		h.Link = p.URL
		h.Reason = hcReason(h.Status)
		data.Checks = append(data.Checks, h)
	}
	return data
}

func init() {
	registerHeartbeat(enums.ServiceHealthchecks)
	Register(HealthchecksData)
	Register(testOf{HealthchecksData, func(d any) map[string]any {
		data := d.(*HealthchecksDataset)
		return map[string]any{"checks": len(data.Checks), "down": len(data.Down())}
	}})
}
