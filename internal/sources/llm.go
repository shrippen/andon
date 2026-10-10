package sources

// LLM providers: what the tokens cost, per day and model, and how much
// credit, key limit or plan quota is left. Every provider fills the same
// shape, so one tile and the rules read them all, also providers added
// later:
//
//	claudeapi   Usage & Cost Admin API      tokens + USD per day and model
//	claudeplan  claude.ai plan (OAuth)      session and week quota in %
//	openrouter  key, credits, activity      spend, key limit, credit, per model (management key)
//	openai      organization usage + costs  tokens per model, USD per day
//	deepseek    user balance                balance only
//	litellm     daily activity (proxy)      USD and tokens per day and model
//
//	ClaudeAPIData, OpenRouterData, … ─► *LLMDataset{Accounts: [one per connection]}

import (
	"slices"
	"time"

	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

// llmDays is how far back the history reaches: always the whole month.
const llmDays = 31

// QuotaKind is a plan quota window.
type QuotaKind string

const (
	QuotaSession   QuotaKind = "session"    // rolling session window (5 h)
	QuotaWeek      QuotaKind = "week"       // all models, 7 days
	QuotaWeekModel QuotaKind = "week_model" // one model family, 7 days
	QuotaExtra     QuotaKind = "extra"      // paid extra usage of the month
)

// LLMUse is one model's use on one day.
type LLMUse struct {
	Day      string // 2006-01-02, UTC
	Model    string // "" = not per model (web search, a day's total)
	Input    int64  // uncached input tokens
	Output   int64
	Cached   int64 // input tokens read from the cache
	Requests int64
	Cost     float64 // USD
}

// LLMQuota is how full one plan window is.
type LLMQuota struct {
	Kind    QuotaKind
	Model   string // QuotaWeekModel: "Opus", "Sonnet"
	Percent float64
	Resets  time.Time // zero = unknown
}

// LLMAccount is one connection: an API key, organization or plan.
type LLMAccount struct {
	Service enums.ServiceType
	Name    string
	URL     string // the provider's usage page
	Uses    []LLMUse

	Spend        bool    // the provider reports spend at all
	Today, Month float64 // USD, from Uses unless the provider names them

	HasBalance bool // prepaid credit
	Balance    float64
	Currency   string

	Limit      float64 // key spend limit in USD, 0 = none
	LimitLeft  float64
	LimitReset string // "monthly", "daily", "" = never

	Quotas []LLMQuota
	Budget float64 // monthly USD set on the connection, 0 = none
}

// LLMDataset is every account of the space's LLM connections.
type LLMDataset struct {
	Accounts []LLMAccount
}

func (d *LLMDataset) Merge(o any) any {
	x, ok := o.(*LLMDataset)
	if !ok {
		return d
	}
	return &LLMDataset{Accounts: slices.Concat(d.Accounts, x.Accounts)}
}

// llmServices are the services whose dataset is an *LLMDataset.
var llmServices = []enums.ServiceType{enums.ServiceClaudeAPI, enums.ServiceClaudePlan, enums.ServiceOpenRouter,
	enums.ServiceOpenAI, enums.ServiceDeepSeek, enums.ServiceLiteLLM}

// LLMServices lists the LLM providers, for the tile's queries.
func LLMServices() []enums.ServiceType { return slices.Clone(llmServices) }

// budgetOption is the connection's monthly budget in USD.
const budgetOption = "budget"

// newAccount starts an account for a connection.
func newAccount(service enums.ServiceType, sctx Ctx, name string) LLMAccount {
	return LLMAccount{Service: service, Name: name, Budget: asFloat(sctx.Options[budgetOption])}
}

// sumUses sets Today and Month from Uses (UTC days).
func (a *LLMAccount) sumUses(now time.Time) {
	today := now.UTC().Format(time.DateOnly)
	month := today[:len("2006-01")]
	a.Today, a.Month = 0, 0
	for _, u := range a.Uses {
		if u.Day == today {
			a.Today += u.Cost
		}
		if u.Day[:len(month)] == month {
			a.Month += u.Cost
		}
	}
}

// historyStart is the first day the history asks for, at 00:00 UTC.
func historyStart(now time.Time) time.Time {
	y, m, d := now.UTC().AddDate(0, 0, 1-llmDays).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// DemoLLM is one provider's accounts in Studio Weber's world: per model
// a value per day, oldest first, the last one today.
func DemoLLM(service enums.ServiceType, now time.Time) *LLMDataset {
	var p struct {
		Accounts []struct {
			Provider, Name, URL, Currency, LimitReset string
			Budget, Limit, Balance                    float64
			Models                                    []struct {
				Model                           string
				Cost                            []float64
				Input, Output, Cached, Requests []int64
			}
			Windows []struct {
				Kind    QuotaKind
				Model   string
				Percent float64
				Resets  time.Time
			}
		}
	}
	demoworld.MustDecode("llm_usage", now, &p)
	data := &LLMDataset{}
	for _, raw := range p.Accounts {
		if raw.Provider != string(service) {
			continue
		}
		a := LLMAccount{Service: service, Name: raw.Name, URL: raw.URL, Budget: raw.Budget, Spend: len(raw.Models) > 0,
			HasBalance: raw.Balance > 0, Balance: raw.Balance, Currency: raw.Currency, Limit: raw.Limit, LimitReset: raw.LimitReset}
		for _, m := range raw.Models {
			for i, cost := range m.Cost {
				day := now.UTC().AddDate(0, 0, i+1-len(m.Cost)).Format(time.DateOnly)
				a.Uses = append(a.Uses, LLMUse{Day: day, Model: m.Model, Cost: cost, Input: at(m.Input, i), Output: at(m.Output, i),
					Cached: at(m.Cached, i), Requests: at(m.Requests, i)})
			}
		}
		for _, w := range raw.Windows {
			a.Quotas = append(a.Quotas, LLMQuota(w))
		}
		a.sumUses(now)
		if a.Limit > 0 {
			a.LimitLeft = max(0, a.Limit-a.Month)
		}
		data.Accounts = append(data.Accounts, a)
	}
	return data
}

// at is list[i], 0 past its end.
func at(list []int64, i int) int64 {
	if i < len(list) {
		return list[i]
	}
	return 0
}

func init() {
	for _, s := range []source{ClaudeAPIData, ClaudePlanData, OpenRouterData, OpenAIData, DeepSeekData, LiteLLMData} {
		Register(s)
		Register(testOf{s, llmTest})
	}
}

// llmTest is what a connection test reports: models, spend, quota.
func llmTest(d any) map[string]any {
	out := map[string]any{}
	for _, a := range d.(*LLMDataset).Accounts {
		models := map[string]bool{}
		for _, u := range a.Uses {
			models[u.Model] = true
		}
		out["models"] = len(models)
		if a.Spend {
			out["month_usd"] = a.Month
		}
		if a.HasBalance {
			out["balance"] = a.Balance
		}
		if len(a.Quotas) > 0 {
			out["quotas"] = len(a.Quotas)
		}
	}
	return out
}
