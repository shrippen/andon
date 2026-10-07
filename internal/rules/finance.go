package rules

// Depot (Ghostfolio):
//
//	ghostfolio.drawdown   the depot lost more than "percent" over its range
//	cross.depot_reserve   cash falls short of taxes and fixed costs
//	                      (sure.spendable_negative) while the depot could
//	                      close the gap

import (
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

var ghostfolioSvc = string(enums.ServiceGhostfolio)

func init() {
	Register("ghostfolio.drawdown", ghostfolioSvc, map[string]any{"percent": 10.0}, on(depotDrawdown))
	Register("cross.depot_reserve", Cross, nil, depotReserve)
}

func depotDrawdown(data *sources.GhostfolioDataset, cfg map[string]any, _ Env) []Finding {
	if data.PerformancePct > -cfgFloat(cfg, "percent") {
		return nil
	}
	return []Finding{svcFinding(ghostfolioSvc, "ghostfolio.drawdown", "drawdown", "ghostfolio.drawdown", enums.SeverityInfo, data.URL,
		map[string]any{"percent": Num(-data.PerformancePct, 1), "value": Money(data.Value, data.Currency)})}
}

func depotReserve(_ any, _ map[string]any, env Env) []Finding {
	bank, ok1 := metrics.BankOf(env.Datasets)
	ninja, ok2 := env.Datasets[ninjaSvc].(*sources.NinjaDataset)
	depot, ok3 := env.Datasets[ghostfolioSvc].(*sources.GhostfolioDataset)
	if !ok1 || !ok2 || !ok3 {
		return nil
	}
	s := metrics.SafeToSpend(bank, ninja, env.Today, metrics.TaxVATInterval(env.Settings), metrics.TaxVATMethod(env.Settings), taxRate(env.Settings))
	if s.Free >= 0 || depot.Value < -s.Free {
		return nil
	}
	return []Finding{{Fingerprint: "reserve", Severity: enums.SeverityInfo, Message: "cross.depot_reserve",
		Params:  map[string]any{"missing": Money(-s.Free, bank.Currency), "depot": Money(depot.Value, depot.Currency)},
		Sources: []string{string(bank.From()), ghostfolioSvc}}}
}
