package rules

// LLM spend across every provider (sources.LLMDataset):
//
//	llm.budget      the month's spend passed the connection's budget, or
//	                will by the month's end at this pace
//	llm.credit_low  prepaid credit or the key's spend limit runs out
//	llm.spike       today costs a multiple of the days before
//	                (a loop, a batch run twice, a model mixed up)
//	cross.llm_unbilled  an account spent, but neither Invoice Ninja nor
//	                the bank knows a payment to its provider lately
//	                (the provider's invoice was not recorded)

import (
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const usd = "USD"

func init() {
	Register("llm.budget", Cross, nil, llmBudget)
	Register("llm.credit_low", Cross, map[string]any{"min_balance": 5.0, "min_limit_pct": 20.0}, llmCreditLow)
	Register("llm.spike", Cross, map[string]any{"factor": 3.0, "min_usd": 1.0, "days": 14.0}, llmSpike)
	Register("cross.llm_unbilled", Cross, map[string]any{"min_usd": 5.0, "days": 45.0}, llmUnbilled)
}

// llmVendors is the name a provider bills under, as it shows in a
// vendor or a bank booking; a proxy (LiteLLM) bills nothing itself.
var llmVendors = map[enums.ServiceType]string{
	enums.ServiceClaudeAPI:  "Anthropic",
	enums.ServiceOpenAI:     "OpenAI",
	enums.ServiceOpenRouter: "OpenRouter",
	enums.ServiceDeepSeek:   "DeepSeek",
}

// llmUnbilled: spend of the last days without an expense (Invoice Ninja)
// or a payment (bank) to the provider in the same days. Providers bill
// prepaid credit or monthly; either way a payment falls in the window.
func llmUnbilled(_ any, cfg map[string]any, env Env) []Finding {
	ninja, _ := env.Datasets[string(enums.ServiceInvoiceNinja)].(*sources.NinjaDataset)
	bank, _ := metrics.BankOf(env.Datasets)
	if ninja == nil && bank == nil {
		return nil
	}
	days := cfgInt(cfg, "days")
	since := env.Today.AddDate(0, 0, -days)

	var found []Finding
	for _, a := range metrics.LLMAccounts(env.Datasets) {
		vendor := llmVendors[a.Service]
		spent := metrics.LLMSpentSince(a, since)
		if vendor == "" || !a.Spend || spent < cfgFloat(cfg, "min_usd") || llmPaid(ninja, bank, vendor, since) {
			continue
		}
		found = append(found, llmFinding(a, "cross.llm_unbilled", "unbilled", "cross.llm_unbilled", enums.SeverityInfo,
			map[string]any{"provider": vendor, "spent": Money(spent, usd), "days": days}))
	}
	return found
}

// llmPaid says whether an expense or a debit since names the vendor.
func llmPaid(ninja *sources.NinjaDataset, bank *sources.BankData, vendor string, since time.Time) bool {
	recent := func(day string) bool {
		d, ok := metrics.ParseDay(day)
		return ok && !d.Before(since)
	}
	names := func(text string) bool { return strings.Contains(strings.ToLower(text), strings.ToLower(vendor)) }

	if ninja != nil {
		vendors := map[string]string{}
		for _, v := range ninja.Vendors {
			vendors[v.Key] = v.Name
		}
		for _, x := range ninja.Expenses {
			if recent(x.Date) && names(vendors[x.VendorKey]) {
				return true
			}
		}
	}
	if bank != nil {
		for _, t := range bank.Transactions {
			if t.Amount < 0 && recent(t.Date) && names(t.Merchant+" "+t.Name) {
				return true
			}
		}
	}
	return false
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
