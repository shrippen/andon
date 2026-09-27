package rules

import (
	"math"
	"sort"

	"andon/internal/enums"
)

// ClockDataset is the Env.Datasets key of the space's service hosts with
// their clock skew, measured on every HTTP answer (Date header).
const ClockDataset = "clocks"

// Clock is one service host and how far its clock is off.
type Clock struct {
	Name, Host string
	Seconds    float64 // negative: behind
}

func init() {
	// A clock off by minutes breaks TOTP codes, certificate checks and
	// backup schedules long before anyone notices the time.
	Register("system.clock_skew", Cross, map[string]any{"seconds": 60.0}, func(_ any, cfg map[string]any, env Env) []Finding {
		clocks, _ := env.Datasets[ClockDataset].([]Clock)
		sort.Slice(clocks, func(i, j int) bool { return clocks[i].Host < clocks[j].Host })
		var found []Finding
		for _, c := range clocks {
			if math.Abs(c.Seconds) < cfgFloat(cfg, "seconds") {
				continue
			}
			found = append(found, Finding{Fingerprint: "clock:" + c.Host, Rule: "system.clock_skew",
				Severity: enums.SeverityWarn, Message: "system.clock_skew",
				Params: map[string]any{"name": c.Name, "host": c.Host, "seconds": int(c.Seconds)}})
		}
		return found
	})
}
