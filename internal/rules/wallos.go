package rules

import (
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

// longCycle tells a quarterly or yearly subscription from a monthly one:
// its price covers at least this many months.
const longCycle = 2.5

func init() {
	svc := string(enums.ServiceWallos)

	// A yearly subscription renews once: the weeks before are the only
	// chance to cancel it. Monthly ones renew all the time and stay quiet.
	Register("wallos.renewal_soon", svc, map[string]any{"days": 30.0}, func(raw any, cfg map[string]any, env Env) []Finding {
		data, _ := raw.(*sources.WallosDataset)
		horizon := env.Today.AddDate(0, 0, int(cfgFloat(cfg, "days")))
		var found []Finding
		for _, s := range data.Subs {
			next, err := time.Parse(time.DateOnly, s.Next)
			if err != nil || s.Inactive || s.Monthly <= 0 || s.Price/s.Monthly < longCycle {
				continue
			}
			if next.Before(env.Today) || next.After(horizon) {
				continue
			}
			f := svcFinding(svc, "wallos.renewal_soon", "renew:"+s.Name+":"+s.Next, "wallos.renewal_soon",
				enums.SeverityInfo, data.URL, map[string]any{"name": s.Name, "amount": Money(s.Price, data.Currency), "day": Day(next)})
			f.Due = s.Next
			found = append(found, f)
		}
		return found
	})
}
