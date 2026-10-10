package metrics

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

// TestLLMPace: 20 $ by the 10th of October (31 days) → 62 $.
func TestLLMPace(t *testing.T) {
	if got := LLMPace(20, time.Date(2026, 10, 10, 15, 0, 0, 0, time.UTC)); got != 62 {
		t.Fatalf("pace %v", got)
	}
}

// TestLLMDailyAndModels: days sum over models; days outside the window
// drop; models of last month stay out of the month's table.
func TestLLMDailyAndModels(t *testing.T) {
	today := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	a := sources.LLMAccount{Service: enums.ServiceOpenAI, Uses: []sources.LLMUse{
		{Day: "2026-09-30", Model: "old", Cost: 5, Input: 10},
		{Day: "2026-10-01", Model: "a", Cost: 1, Input: 100, Output: 10, Requests: 2},
		{Day: "2026-10-02", Model: "a", Cost: 2, Input: 200},
		{Day: "2026-10-02", Model: "b", Cost: 0.5, Input: 900},
	}}
	if got := LLMDaily(a, today, 2); got[0] != 1 || got[1] != 2.5 {
		t.Fatalf("daily %v", got)
	}
	rows := LLMModels([]sources.LLMAccount{a}, today)
	if len(rows) != 2 || rows[0].Model != "a" || rows[0].Cost != 3 || rows[0].Input != 300 || rows[0].Service != "openai" {
		t.Fatalf("models %+v", rows)
	}
	if tot := LLMTotalsOf([]sources.LLMAccount{a}, today); tot.Tokens != 1210 || tot.Requests != 2 {
		t.Fatalf("totals %+v", tot)
	}
}
