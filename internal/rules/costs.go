package rules

// Cost rules of the homelab (phase 13):
//
//	system.unused_service   running stack, guest or app nobody used for months
//	energy.shift_jobs       backups run in expensive hours; cheapest hour named
//	energy.weather_adjusted last week used more power than the weather explains

import (
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const bytesPerGB = 1 << 30

func init() {
	Register("system.unused_service", Cross, map[string]any{"days": 90.0}, unusedService)

	// A failed inventory or usage source would read as "nothing unused"
	// and resolve every hint until the next run.
	Needs("system.unused_service", string(enums.ServiceKomodo), string(enums.ServiceProxmox), string(enums.ServiceTrueNAS),
		string(enums.ServiceAuthentik))

	Register("energy.shift_jobs", Cross, map[string]any{"min_saving": 0.05}, shiftJobs)

	Register("energy.weather_adjusted", Cross, map[string]any{"share": 0.15}, weatherAdjusted)
}

func unusedService(_ any, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, u := range metrics.UnusedServices(env.Datasets, Usages(env), env.Today, cfgInt(cfg, "days")) {
		// Only Proxmox names a guest's memory; "0,0 GB" would be wrong.
		msg := "system.unused_service"
		if u.MemBytes == 0 {
			msg = "system.unused_service_nomem"
		}
		found = append(found, Finding{Fingerprint: "unused:" + u.Kind + ":" + strings.ToLower(u.Name), Severity: enums.SeverityInfo, Message: msg,
			Params:  map[string]any{"name": u.Name, "kind": u.Kind, "days": cfgInt(cfg, "days"), "gb": Num(u.MemBytes/bytesPerGB, 1)},
			Sources: []string{system}})
	}
	return found
}

func shiftJobs(_ any, cfg map[string]any, env Env) []Finding {
	tibber, ok := env.Datasets[string(enums.ServiceTibber)].(*sources.TibberDataset)
	if !ok {
		return nil
	}
	var found []Finding
	for _, j := range metrics.JobPrices(tibber, env.Datasets) {
		if j.Price-j.CheapPrice < cfgFloat(cfg, "min_saving") {
			continue
		}
		found = append(found, Finding{Fingerprint: "shift:" + j.Job, Severity: enums.SeverityInfo,
			Message: "energy.shift_jobs", Params: map[string]any{"job": j.Job, "hour": j.Hour, "cheap_hour": j.CheapHour,
				"price": Money(j.Price, tibber.Currency), "cheap_price": Money(j.CheapPrice, tibber.Currency)},
			Sources: []string{string(enums.ServiceTibber)}})
	}
	return found
}

func weatherAdjusted(_ any, cfg map[string]any, env Env) []Finding {
	tibber, ok := env.Datasets[string(enums.ServiceTibber)].(*sources.TibberDataset)
	if !ok {
		return nil
	}
	w, ok := metrics.WeatherAdjusted(tibber.Days)
	if !ok || w.Deviation < cfgFloat(cfg, "share") {
		return nil
	}
	return []Finding{{Fingerprint: "weather:" + metrics.WeekStart(env.Today).Format(time.DateOnly), Severity: enums.SeverityInfo, Message: "energy.weather_adjusted",
		Params:  map[string]any{"actual": Num(w.ActualKWh, 0), "expected": Num(w.ExpectedKWh, 0), "percent": Num(w.Deviation*100, 0)},
		Sources: []string{string(enums.ServiceTibber)}}}
}
