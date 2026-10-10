package rules

// LLM spend across every provider (sources.LLMDataset):
//
//	llm.budget      the month's spend passed the connection's budget, or
//	                will by the month's end at this pace
//	llm.credit_low  prepaid credit or the key's spend limit runs out
//	llm.quota_high  a plan window (session, week) is nearly used up
//	llm.spike       today costs a multiple of the days before
//	                (a loop, a batch run twice, a model mixed up)

import (
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const usd = "USD"

func init() {
	Register("llm.budget", Cross, nil, llmBudget)
	Register("llm.credit_low", Cross, map[string]any{"min_balance": 5.0, "min_limit_pct": 20.0}, llmCreditLow)
	Register("llm.quota_high", Cross, map[string]any{"percent": 85.0}, llmQuotaHigh)
	Register("llm.spike", Cross, map[string]any{"factor": 3.0, "min_usd": 1.0, "days": 14.0}, llmSpike)
}

// llmFinding is a hint on one account, with the provider's page as action.
func llmFinding(a sources.LLMAccount, rule, fp, msg string, level enums.Severity, params map[string]any) Finding {
	params["account"] = accountName(a)
	return svcFinding(string(a.Service), rule, string(a.Service)+":"+a.Name+":"+fp, msg, level, a.URL, params)
}

// accountName is the account's own name, else its provider's.
func accountName(a sources.LLMAccount) any {
	if a.Name != "" {
		return a.Name
	}
	return map[string]any{"$t": "service." + string(a.Service)}
}

func llmBudget(_ any, _ map[string]any, env Env) []Finding {
	var found []Finding
	for _, a := range metrics.LLMAccounts(env.Datasets) {
		if a.Budget <= 0 || !a.Spend {
			continue
		}
		params := map[string]any{"spent": Money(a.Month, usd), "budget": Money(a.Budget, usd)}
		if a.Month >= a.Budget {
			found = append(found, llmFinding(a, "llm.budget", "over", "llm.budget_over", enums.SeverityWarn, params))
			continue
		}
		if pace := metrics.LLMPace(a.Month, env.Today); pace > a.Budget {
			params["pace"] = Money(pace, usd)
			found = append(found, llmFinding(a, "llm.budget", "pace", "llm.budget_pace", enums.SeverityInfo, params))
		}
	}
	return found
}

func llmCreditLow(_ any, cfg map[string]any, env Env) []Finding {
	minBalance, minPct := cfgFloat(cfg, "min_balance"), cfgFloat(cfg, "min_limit_pct")
	var found []Finding
	for _, a := range metrics.LLMAccounts(env.Datasets) {
		if a.HasBalance && a.Balance < minBalance {
			found = append(found, llmFinding(a, "llm.credit_low", "credit", "llm.credit_low", enums.SeverityWarn,
				map[string]any{"balance": Money(a.Balance, a.Currency)}))
		}
		if a.Limit > 0 && a.LimitLeft/a.Limit*100 < minPct {
			found = append(found, llmFinding(a, "llm.credit_low", "limit", "llm.limit_low", enums.SeverityWarn,
				map[string]any{"left": Money(a.LimitLeft, usd), "limit": Money(a.Limit, usd)}))
		}
	}
	return found
}

func llmQuotaHigh(_ any, cfg map[string]any, env Env) []Finding {
	limit := cfgFloat(cfg, "percent")
	var found []Finding
	for _, a := range metrics.LLMAccounts(env.Datasets) {
		for _, q := range a.Quotas {
			if q.Percent < limit {
				continue
			}
			window := map[string]any{"$t": "llm.quota." + string(q.Kind), "args": map[string]any{"model": q.Model}}
			params := map[string]any{"window": window, "percent": Num(q.Percent, 0), "resets": "–"}
			if !q.Resets.IsZero() {
				params["resets"] = map[string]any{"$ago": q.Resets.Format(time.RFC3339)}
			}
			found = append(found, llmFinding(a, "llm.quota_high", string(q.Kind)+":"+q.Model, "llm.quota_high", enums.SeverityInfo, params))
		}
	}
	return found
}

func llmSpike(_ any, cfg map[string]any, env Env) []Finding {
	factor, minUSD, days := cfgFloat(cfg, "factor"), cfgFloat(cfg, "min_usd"), int(cfgFloat(cfg, "days"))
	var found []Finding
	for _, a := range metrics.LLMAccounts(env.Datasets) {
		now, mean, ok := metrics.LLMSpike(a, env.Today, days)
		if !ok || now < minUSD || now < factor*mean {
			continue
		}
		found = append(found, llmFinding(a, "llm.spike", env.Today.Format("2006-01-02"), "llm.spike", enums.SeverityInfo,
			map[string]any{"today": Money(now, usd), "usual": Money(mean, usd)}))
	}
	return found
}
