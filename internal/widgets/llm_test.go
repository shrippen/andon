package widgets

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

// llmDemo is every provider's demo dataset, keyed like the tile's queries.
func llmDemo() map[string]any {
	out := map[string]any{}
	for _, s := range sources.LLMServices() {
		out[string(s)] = sources.DemoLLM(s, time.Now())
	}
	return out
}

// TestLLMView: the tile sums the month over every account, troubled
// ones first (the plan's week is past 85 %); the provider filter keeps
// its accounts.
func TestLLMView(t *testing.T) {
	v := llmView(LLMConfig{}, llmDemo(), ViewCtx{})
	lines := v["Lines"].([]LLMLine)
	if len(lines) != 6 || v["Trouble"] != true || lines[0].State == "" || v["Month"].(float64) <= 0 {
		t.Fatalf("view %+v", v)
	}
	v = llmView(LLMConfig{Providers: []string{"deepseek"}}, llmDemo(), ViewCtx{})
	if lines := v["Lines"].([]LLMLine); len(lines) != 1 || lines[0].Credit == nil || v["Spends"] != false {
		t.Fatalf("filter %+v", v)
	}
	if v := llmView(LLMConfig{}, map[string]any{}, ViewCtx{}); v["Any"] != nil {
		t.Fatalf("empty %+v", v)
	}
}

// TestLLMDetail: the chart has a line with a legend per account with
// history (DeepSeek and the plan have none), the quotas and limits bars,
// the models.
func TestLLMDetail(t *testing.T) {
	body := llmDetail(LLMConfig{}, llmDemo(), ViewCtx{}).Body.(*DetailBody)
	g := body.Blocks[0].Data.(Graph)
	if len(g.Series) != 4 || g.Series[0].Label == nil || len(g.Labels) != llmChartDays || g.Unit != "$" {
		t.Fatalf("chart %+v", g)
	}
	bars := body.Blocks[1].Data.([]ShareBar)
	if len(bars) != 5 { // plan: 4 windows; OpenRouter: key limit
		t.Fatalf("bars %+v", bars)
	}
	if body.Blocks[3].Data.(Table).Rows == nil {
		t.Fatal("no models")
	}
	if line := llmLine(sources.LLMAccount{Service: enums.ServiceClaudeAPI, Spend: true, Month: 70, Budget: 60}); line.State != pillFailed {
		t.Fatalf("over budget %+v", line)
	}
}
