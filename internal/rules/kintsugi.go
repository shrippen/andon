package rules

//	kintsugi.run_failed   the last daily suggestion run failed
//	kintsugi.budget       the monthly research budget is used up
//	kintsugi.stale        open suggestions wait longer than "days"

import (
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

func init() {
	registerKintsugi()
}

func registerKintsugi() {
	// One hint per failed day: a failure after an acknowledged one reopens.
	Register("kintsugi.run_failed", kintsugiSvc, nil, on(runFailed))

	// Research stops for the month once the budget is spent; the
	// suggestions then come from the profile alone.
	Register("kintsugi.budget", kintsugiSvc, nil, on(kintsugiBudget))

	Register("kintsugi.stale", kintsugiSvc, map[string]any{"days": 7.0}, on(kintsugiStale))
}

func runFailed(data *sources.KintsugiDataset, _ map[string]any, _ Env) []Finding {
	run := data.LastRun
	if run == nil || run.Status != sources.KintsugiRunFailed {
		return nil
	}
	return []Finding{svcFinding(kintsugiSvc, "kintsugi.run_failed", "run:"+run.At.Format(time.DateOnly), "kintsugi.run_failed",
		enums.SeverityWarn, data.URL+"/vorschlaege", map[string]any{"detail": run.Detail})}
}

func kintsugiBudget(data *sources.KintsugiDataset, _ map[string]any, env Env) []Finding {
	if !data.Research || data.BudgetUSD <= 0 || data.UsedUSD < data.BudgetUSD {
		return nil
	}
	return []Finding{svcFinding(kintsugiSvc, "kintsugi.budget", "budget:"+env.Today.Format("2006-01"), "kintsugi.budget",
		enums.SeverityInfo, data.URL+"/einstellungen", map[string]any{"budget": data.BudgetUSD})}
}

func kintsugiStale(data *sources.KintsugiDataset, cfg map[string]any, env Env) []Finding {
	oldest, ok := data.Oldest()
	if !ok {
		return nil
	}
	days := int(env.Today.Sub(oldest).Hours() / hoursPerDay)
	if days < cfgInt(cfg, "days") {
		return nil
	}
	return []Finding{svcFinding(kintsugiSvc, "kintsugi.stale", "stale", "kintsugi.stale",
		enums.SeverityInfo, data.URL+"/vorschlaege", map[string]any{"count": data.New, "days": days})}
}
