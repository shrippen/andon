package widgets

// "llm_usage": what the LLM providers cost this month, over every
// connection of the space that is an LLM provider (Claude API and plan,
// OpenRouter, OpenAI, DeepSeek, LiteLLM). The tile sums the month and
// lists each account: its spend, credit or plan quota. The dialog draws
// the cost per day and provider, the models and every account.
//
//	tile:    12,40 $   this month · today 4,60 $
//	         Claude API     8,10 $   today 4,60 $
//	         Claude plan    week ████████▉ 91 %
//	         DeepSeek       credit 2,40 $

import (
	"math"
	"slices"
	"strings"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

const (
	llmUSD       = "USD"
	llmChartDays = 30 // dialog chart
	llmQuotaWarn = 85.0
	llmQuotaFull = 100.0
)

// llmClasses are Kante's data colours, one per account in the chart.
var llmClasses = []string{"s1", "s2", "s3", "s4", "s5", "s6"}

// LLMConfig is the "llm_usage" widget's config.
type LLMConfig struct {
	Providers []string // only these services (keys, lower case), empty = all
}

func init() {
	Tile[LLMConfig]{Key: "llm_usage", Detail: llmDetail, Category: CategoryInsight, Topic: TopicDev, RefreshS: 600,
		Fields:  []Field{{Key: "providers", Input: InputList}},
		Decode:  func(r Raw) LLMConfig { return LLMConfig{Providers: r.Lower("providers")} },
		Queries: llmQueries, View: llmView,
		Calm: func(v map[string]any) bool { return v["Any"] == true && v["Trouble"] == false }}.add()
}

func llmQueries(LLMConfig) []Query {
	var out []Query
	for _, s := range sources.LLMServices() {
		out = append(out, Query{Name: string(s), Source: "data", Conn: ConnPeer, Service: s})
	}
	return out
}

// llmAccounts are the accounts the config picks.
func llmAccounts(cfg LLMConfig, results map[string]any) []sources.LLMAccount {
	var out []sources.LLMAccount
	for _, a := range metrics.LLMAccounts(results) {
		if len(cfg.Providers) == 0 || slices.Contains(cfg.Providers, string(a.Service)) {
			out = append(out, a)
		}
	}
	return out
}

// LLMLine is one account on the tile.
type LLMLine struct {
	Service string
	Name    string
	Spend   bool
	Month   float64
	Today   float64
	Credit  *LLMCredit
	Quota   *LLMMeter // the fullest plan window
	Limit   *LLMMeter // share of the key's spend limit used
	State   string    // Kante pill state: failed, locked (warning), "" calm
}

// LLMCredit is prepaid credit left.
type LLMCredit struct {
	Amount   float64
	Currency string
}

// LLMMeter is a bar: label key, share used in per cent (Fill capped at
// 100 for the bar), its tier.
type LLMMeter struct {
	Key   string
	Model string
	Pct   float64
	Fill  float64
	Tier  string
}

// meterOf is a bar for a share used.
func meterOf(key, model string, pct float64) *LLMMeter {
	return &LLMMeter{Key: key, Model: model, Pct: pct, Fill: math.Round(min(pct, percentScale)), Tier: quotaTier(pct)}
}

func llmView(cfg LLMConfig, results map[string]any, ctx ViewCtx) map[string]any {
	accounts := llmAccounts(cfg, results)
	if len(accounts) == 0 {
		return map[string]any{}
	}
	today := todayOf(ctx)
	totals := metrics.LLMTotalsOf(accounts, today)

	var lines []LLMLine
	trouble := false
	for _, a := range accounts {
		line := llmLine(a)
		trouble = trouble || line.State != ""
		lines = append(lines, line)
	}
	slices.SortStableFunc(lines, func(x, y LLMLine) int { return llmStateRank(x.State) - llmStateRank(y.State) })
	return map[string]any{"Any": true, "Trouble": trouble, "Month": totals.Month, "Today": totals.Today, "Currency": llmUSD,
		"Spends": slices.ContainsFunc(accounts, func(a sources.LLMAccount) bool { return a.Spend }), "Lines": lines}
}

// llmLine shapes an account; State says whether it needs a look.
func llmLine(a sources.LLMAccount) LLMLine {
	line := LLMLine{Service: string(a.Service), Name: a.Name, Spend: a.Spend, Month: a.Month, Today: a.Today}
	if a.HasBalance {
		line.Credit = &LLMCredit{Amount: a.Balance, Currency: a.Currency}
	}
	if q, ok := metrics.LLMQuotaTop(a); ok {
		line.Quota = meterOf("llm.quota."+string(q.Kind), q.Model, q.Percent)
		line.State = llmPill(line.Quota.Tier)
	}
	if a.Limit > 0 {
		used := (a.Limit - a.LimitLeft) / a.Limit * percentScale
		line.Limit = meterOf("llm.limit_used", "", used)
		line.State = worstPill(line.State, llmPill(line.Limit.Tier))
	}
	if a.Budget > 0 && a.Month >= a.Budget {
		line.State = worstPill(line.State, pillFailed)
	}
	return line
}

// Kante pill states for a line.
const (
	pillFailed = "failed"
	pillWarn   = "locked"
)

// quotaTier colours a share used: green, yellow from llmQuotaWarn, red when full.
func quotaTier(pct float64) string {
	switch {
	case pct >= llmQuotaFull:
		return "red"
	case pct >= llmQuotaWarn:
		return "yellow"
	}
	return "green"
}

func llmPill(tier string) string {
	switch tier {
	case "red":
		return pillFailed
	case "yellow":
		return pillWarn
	}
	return ""
}

func worstPill(a, b string) string {
	if llmStateRank(b) < llmStateRank(a) {
		return b
	}
	return a
}

func llmStateRank(state string) int {
	switch state {
	case pillFailed:
		return 0
	case pillWarn:
		return 1
	}
	return 2
}

// llmLabel names an account in the dialog: "Claude API · Studio Weber".
func llmLabel(a sources.LLMAccount) any {
	if a.Name == "" {
		return Txt("service." + string(a.Service))
	}
	return TxtA("llm.account", "service", Txt("service."+string(a.Service)), "name", a.Name)
}

// llmShort names an account in a bar, where room is short: its own name,
// else its provider's.
func llmShort(a sources.LLMAccount) any {
	if a.Name == "" {
		return Txt("service." + string(a.Service))
	}
	return a.Name
}

func llmDetail(cfg LLMConfig, results map[string]any, ctx ViewCtx) DetailView {
	accounts := llmAccounts(cfg, results)
	body := &DetailBody{}
	if len(accounts) == 0 {
		body.Blocks = []Block{{Kind: BlockText, Data: Txt("llm.none")}}
		return DetailView{Body: body}
	}
	today := todayOf(ctx)
	totals := metrics.LLMTotalsOf(accounts, today)
	body.Facts = []Kpi{{Value: Money(totals.Month, llmUSD), Label: T("llm.facts.month")}, {Value: Money(totals.Today, llmUSD), Label: T("llm.facts.today")},
		{Value: Num(float64(totals.Tokens), 0), Label: T("llm.facts.tokens")}, {Value: Num(float64(totals.Requests), 0), Label: T("llm.facts.requests")}}

	if g, ok := llmChart(accounts, today); ok {
		body.Blocks = append(body.Blocks, Block{Kind: BlockGraph, Label: T("llm.chart"), Hero: true, Data: g})
	}
	if bars := llmBars(accounts); len(bars) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockBars, Label: T("llm.quotas"), Data: bars})
	}
	body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("llm.accounts"), Data: llmAccountTable(accounts)})
	if models := metrics.LLMModels(accounts, today); len(models) > 0 {
		body.Blocks = append(body.Blocks, Block{Kind: BlockTable, Label: T("llm.models"), Data: llmModelTable(models)})
	}
	body.Blocks = append(body.Blocks, hintsBlock(results)...)
	return DetailView{Body: body}
}

// llmChart is the cost per day, one line per account with history.
func llmChart(accounts []sources.LLMAccount, today time.Time) (Graph, bool) {
	var series []Series
	for _, a := range accounts {
		if len(a.Uses) == 0 {
			continue
		}
		class := llmClasses[len(series)%len(llmClasses)]
		series = append(series, Series{Values: metrics.LLMDaily(a, today, llmChartDays), Class: class, Label: llmLabel(a)})
	}
	if len(series) == 0 {
		return Graph{}, false
	}
	first := today.AddDate(0, 0, 1-llmChartDays)
	g := LineGraph(series...)
	g.Unit = "$"
	g.Ticks = []any{Day(first), Txt("detail.today")}
	g.Labels = dayLabels(today, llmChartDays)
	return g, true
}

// llmBars are the plan windows and key limits as share bars.
func llmBars(accounts []sources.LLMAccount) []ShareBar {
	var out []ShareBar
	for _, a := range accounts {
		for _, q := range a.Quotas {
			name := TxtA("llm.bar", "account", llmShort(a), "window", TxtA("llm.quota."+string(q.Kind), "model", q.Model))
			value := NumU(q.Percent, 0, "%")
			if !q.Resets.IsZero() {
				value = TxtA("llm.resets", "pct", Num(q.Percent, 0), "when", agoOf(q.Resets))
			}
			out = append(out, ShareBar{Name: name, Pct: min(q.Percent, percentScale), Value: value, Tier: quotaTier(q.Percent)})
		}
		if a.Limit > 0 {
			used := (a.Limit - a.LimitLeft) / a.Limit * percentScale
			out = append(out, ShareBar{Name: TxtA("llm.bar", "account", llmShort(a), "window", Txt("llm.limit_used")), Pct: min(used, percentScale),
				Value: TxtA("llm.left_of", "left", Money(a.LimitLeft, llmUSD), "limit", Money(a.Limit, llmUSD)), Tier: quotaTier(used)})
		}
	}
	return out
}

// llmAccountTable: account, month, today, credit, budget.
func llmAccountTable(accounts []sources.LLMAccount) Table {
	var rows [][]Cell
	for _, a := range accounts {
		month, todayCost, credit, budget := Cell{Value: "–"}, Cell{Value: "–"}, Cell{Value: "–"}, Cell{Value: "–"}
		if a.Spend {
			month, todayCost = Cell{Value: Money(a.Month, llmUSD)}, Cell{Value: Money(a.Today, llmUSD)}
		}
		if a.HasBalance {
			credit = Cell{Value: Money(a.Balance, a.Currency)}
		}
		if a.Budget > 0 {
			budget = Cell{Value: Money(a.Budget, llmUSD), State: stateIf(a.Month >= a.Budget, "bad")}
		}
		rows = append(rows, []Cell{{Value: llmLabel(a), Href: a.URL}, month, todayCost, credit, budget})
	}
	return Table{Head: []Text{T("llm.col.account"), T("llm.col.month"), T("llm.col.today"), T("llm.col.credit"), T("llm.col.budget")},
		Rows: rows, Num: []int{1, 2, 3, 4}}
}

// llmModelTable: the month per model, most expensive first.
func llmModelTable(models []metrics.LLMModelRow) Table {
	var rows [][]Cell
	for _, m := range models {
		model := any(m.Model)
		if strings.TrimSpace(m.Model) == "" {
			model = Txt("llm.other")
		}
		rows = append(rows, []Cell{{Value: Txt("service." + m.Service)}, {Value: model}, {Value: Num(float64(m.Input), 0)}, {Value: Num(float64(m.Cached), 0)},
			{Value: Num(float64(m.Output), 0)}, {Value: Num(float64(m.Requests), 0)}, {Value: Money(m.Cost, llmUSD)}})
	}
	return Table{Head: []Text{T("llm.col.provider"), T("llm.col.model"), T("llm.col.input"), T("llm.col.cached"), T("llm.col.output"),
		T("llm.col.requests"), T("llm.col.cost")}, Rows: rows, Num: []int{2, 3, 4, 5, 6}}
}
