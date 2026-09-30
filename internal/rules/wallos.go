package rules

import (
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

// longCycle tells a quarterly or yearly subscription from a monthly one:
// its price covers at least this many months.
const longCycle = 2.5

// renewalCfg: how many days ahead a renewal shows.
type renewalCfg struct {
	Days float64 `json:"days"`
}

func init() {
	// A yearly subscription renews once: the weeks before are the only
	// chance to cancel it. Monthly ones renew all the time and stay quiet.
	registerTyped("wallos.renewal_soon", enums.ServiceWallos, renewalCfg{Days: 30}, renewalSoon)
}

func renewalSoon(data *sources.WallosDataset, cfg renewalCfg, env Env) []Finding {
	horizon := env.Today.AddDate(0, 0, int(cfg.Days))
	var found []Finding
	for _, s := range data.Subs {
		next, err := time.Parse(time.DateOnly, s.Next)
		if err != nil || s.Inactive || s.Monthly <= 0 || s.Price/s.Monthly < longCycle {
			continue
		}
		if next.Before(env.Today) || next.After(horizon) {
			continue
		}
		f := svcFinding(wallosSvc, "wallos.renewal_soon", "renew:"+s.Name+":"+s.Next, "wallos.renewal_soon",
			enums.SeverityInfo, data.URL, map[string]any{"name": s.Name, "amount": Money(s.Price, data.Currency), "day": Day(next)})
		f.Due = s.Next
		found = append(found, f)
	}
	return found
}
