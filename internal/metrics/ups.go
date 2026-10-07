package metrics

// UPS against the hosts it carries: a clean shutdown of each host takes
// minutes, the runtime must cover them all. The runtime falls with the
// load, so the history keeps each day's shortest one.
//
//	option hosts: [nas, pve, docker]           every UPS of the connection
//	option hosts: {rack-usv: [nas, pve], …}    per UPS
//	ups.runtime.<name>  the day's lowest runtime in seconds

import (
	"strings"
	"time"

	"andon/internal/sources"
)

// upsHostsOption names the hosts a UPS carries.
const upsHostsOption = "hosts"

// UPSRuntimeKey is the series of a UPS's lowest runtime per day (seconds).
func UPSRuntimeKey(name string) string { return key("ups", "runtime", name) }

// UPSHosts are the hosts a UPS carries, from its connection's options.
func UPSHosts(options map[string]any, name string) []string {
	raw := options[upsHostsOption]
	if per, ok := raw.(map[string]any); ok {
		raw = nil
		for k, v := range per {
			if strings.EqualFold(k, name) {
				raw = v
			}
		}
	}
	list, _ := raw.([]any)
	var out []string
	for _, v := range list {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

// LowestRuntime is the shortest runtime in seconds: now's, or a lower one
// the history kept for the days since today minus days.
func LowestRuntime(h *History, name string, now int, today time.Time, days int) int {
	low := now
	since := today.AddDate(0, 0, -days)
	for _, p := range h.SeriesOf(UPSRuntimeKey(name)) {
		if !p.Day.Before(since) && p.Value > 0 && int(p.Value) < low {
			low = int(p.Value)
		}
	}
	return low
}

func init() {
	Record(func(d *sources.UPSDataset, _ time.Time, r *Readings) {
		for _, u := range d.Devices {
			if u.Runtime > 0 {
				r.Low(UPSRuntimeKey(u.Name), float64(u.Runtime))
			}
		}
	})
}
