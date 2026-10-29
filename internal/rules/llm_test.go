package rules_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

// llmEnv puts accounts of one provider into the cross datasets.
func llmEnv(accounts ...sources.LLMAccount) map[string]any {
	return map[string]any{string(enums.ServiceOpenRouter): &sources.LLMDataset{Accounts: accounts}}
}

// llmDays is cost per day ending today, oldest first.
func llmDays(today time.Time, costs ...float64) []sources.LLMUse {
	var out []sources.LLMUse
	for i, c := range costs {
		out = append(out, sources.LLMUse{Day: today.AddDate(0, 0, i+1-len(costs)).Format(time.DateOnly), Model: "m", Cost: c})
	}
	return out
}

// TestLLMBudget: over the budget warns; below it but heading past it
// at this pace is a note; no budget, nothing.
func TestLLMBudget(t *testing.T) {
	env := todayEnv(nil)
	env.Today = time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	over := sources.LLMAccount{Service: enums.ServiceOpenRouter, Name: "a", Spend: true, Month: 61, Budget: 60}
	pace := sources.LLMAccount{Service: enums.ServiceOpenRouter, Name: "b", Spend: true, Month: 30, Budget: 60} // 30 $ by the 10th → 93 $
	calm := sources.LLMAccount{Service: enums.ServiceOpenRouter, Name: "c", Spend: true, Month: 10, Budget: 60}
	free := sources.LLMAccount{Service: enums.ServiceOpenRouter, Name: "d", Spend: true, Month: 500}
	env.Datasets = llmEnv(over, pace, calm, free)
	got := run(t, "llm.budget", nil, env)
	if len(got) != 2 || got[0].Message != "llm.budget_over" || got[0].Severity != enums.SeverityWarn || got[1].Message != "llm.budget_pace" {
		t.Fatalf("budget %+v", got)
	}
}

// TestLLMCreditLow: credit below the minimum and a key limit nearly
// used each warn.
func TestLLMCreditLow(t *testing.T) {
	env := todayEnv(nil)
	env.Datasets = llmEnv(
		sources.LLMAccount{Service: enums.ServiceOpenRouter, Name: "low", HasBalance: true, Balance: 3.8, Currency: "USD", Limit: 25, LimitLeft: 3},
		sources.LLMAccount{Service: enums.ServiceOpenRouter, Name: "fine", HasBalance: true, Balance: 50, Limit: 25, LimitLeft: 20})
	got := run(t, "llm.credit_low", nil, env)
	if len(got) != 2 || got[0].Message != "llm.credit_low" || got[1].Message != "llm.limit_low" || got[0].Params["account"] != "low" {
		t.Fatalf("credit %+v", got)
	}
}

// TestLLMSpike: today at four times the usual day is a note; a small
// amount or a usual day is not; the demo re-reads receipts today.
func TestLLMSpike(t *testing.T) {
	env := todayEnv(nil)
	usual := []float64{1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1}
	env.Datasets = llmEnv(
		sources.LLMAccount{Service: enums.ServiceOpenRouter, Name: "spike", Uses: llmDays(env.Today, append(usual, 4)...)},
		sources.LLMAccount{Service: enums.ServiceOpenRouter, Name: "usual", Uses: llmDays(env.Today, append(usual, 1.2)...)},
		sources.LLMAccount{Service: enums.ServiceOpenRouter, Name: "small", Uses: llmDays(env.Today, 0.01, 0.9)})
	got := run(t, "llm.spike", nil, env)
	if len(got) != 1 || got[0].Params["account"] != "spike" {
		t.Fatalf("spike %+v", got)
	}

	env.Datasets = map[string]any{string(enums.ServiceClaudeAPI): sources.DemoLLM(enums.ServiceClaudeAPI, time.Now())}
	if got := run(t, "llm.spike", nil, env); len(got) != 1 {
		t.Fatalf("demo spike %+v", got)
	}
}

// TestLLMUnbilled: an account that spent without an expense or a bank
// payment to its provider in paid_days is noted; one Invoice Ninja or
// the bank knows is not (a top-up 100 days ago too), nor one with credit
// left, below the minimum or a proxy.
func TestLLMUnbilled(t *testing.T) {
	today := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	claude := sources.LLMAccount{Service: enums.ServiceClaudeAPI, Name: "Studio", Spend: true, Uses: llmDays(today, 20, 15)}
	credit := sources.LLMAccount{Service: enums.ServiceClaudeAPI, Name: "Guthaben", Spend: true, Uses: llmDays(today, 20), HasBalance: true, Balance: 30}
	small := sources.LLMAccount{Service: enums.ServiceClaudeAPI, Name: "Klein", Spend: true, Uses: llmDays(today, 0.5)}
	openai := sources.LLMAccount{Service: enums.ServiceOpenAI, Spend: true, Uses: llmDays(today, 30)}
	router := sources.LLMAccount{Service: enums.ServiceOpenRouter, Spend: true, Uses: llmDays(today, 12)}
	topped := sources.LLMAccount{Service: enums.ServiceDeepSeek, Spend: true, Uses: llmDays(today, 20)}
	proxy := sources.LLMAccount{Service: enums.ServiceLiteLLM, Spend: true, Uses: llmDays(today, 50)}
	xai := sources.LLMAccount{Service: enums.ServiceXAI, Spend: true, Uses: llmDays(today, 9)}
	ninja := &sources.NinjaDataset{
		Vendors:  []sources.NinjaVendor{{Key: "o", Name: "OpenAI, LLC"}, {Key: "a", Name: "Anthropic PBC"}},
		Expenses: []sources.NinjaExpense{{VendorKey: "o", Date: "2026-10-02", Amount: 28}, {VendorKey: "a", Date: "2025-12-01", Amount: 40}},
	}
	// OpenRouter paid this month; DeepSeek's credit was topped up 100 days
	// ago and still lasts; xAI books as "X.AI LLC".
	bank := &sources.BankData{Transactions: []sources.BankTxn{{Date: "2026-10-05", Name: "OPENROUTER INC", Amount: -12},
		{Date: "2026-07-12", Name: "DEEPSEEK", Amount: -100}, {Date: "2026-09-01", Name: "X.AI LLC", Amount: -25}}}
	env := crossEnv("2026-10-20", map[string]any{
		string(enums.ServiceClaudeAPI):    &sources.LLMDataset{Accounts: []sources.LLMAccount{claude, credit, small}},
		string(enums.ServiceOpenAI):       &sources.LLMDataset{Accounts: []sources.LLMAccount{openai}},
		string(enums.ServiceOpenRouter):   &sources.LLMDataset{Accounts: []sources.LLMAccount{router}},
		string(enums.ServiceDeepSeek):     &sources.LLMDataset{Accounts: []sources.LLMAccount{topped}},
		string(enums.ServiceLiteLLM):      &sources.LLMDataset{Accounts: []sources.LLMAccount{proxy}},
		string(enums.ServiceXAI):          &sources.LLMDataset{Accounts: []sources.LLMAccount{xai}},
		string(enums.ServiceInvoiceNinja): ninja, "sure": bank,
	})
	got := run(t, "cross.llm_unbilled", nil, env)
	if len(got) != 1 || got[0].Params["account"] != "Studio" || got[0].Params["provider"] != "Anthropic" {
		t.Fatalf("unbilled: %+v", got)
	}

	// Without Invoice Ninja and bank there is nothing to compare.
	delete(env.Datasets, string(enums.ServiceInvoiceNinja))
	delete(env.Datasets, "sure")
	if got := run(t, "cross.llm_unbilled", nil, env); len(got) != 0 {
		t.Fatalf("without books: %+v", got)
	}
}
