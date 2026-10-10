package sources

// The LLM providers' APIs, each into an LLMAccount (see llm.go):
//
//	claudeapi   GET v1/organizations/usage_report/messages  x-api-key (admin key)  tokens per day and model
//	            GET v1/organizations/cost_report            cents per day and model
//	claudeplan  GET api/oauth/usage                         Bearer (OAuth token)   {five_hour, seven_day, …} or {limits: […]}
//	openrouter  GET key                                     Bearer                 usage_daily, usage_monthly, limit, …
//	            GET credits, GET activity                   management key only    credit, per day and model
//	openai      GET organization/usage/completions          Bearer (admin key)     tokens per day and model
//	            GET organization/costs                      USD per day and line item
//	deepseek    GET user/balance                            Bearer                 balance_infos
//	litellm     GET user/daily/activity                     Bearer                 results[{date, breakdown.models}]

import (
	"context"
	"maps"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
)

const (
	anthropicVersion = "2023-06-01"
	oauthBeta        = "oauth-2025-04-20"
	centsPerDollar   = 100.0
	llmMaxPages      = 5 // 31 daily buckets fit one page; a few more for many models
	litellmPageSize  = 100
)

var (
	ClaudeAPIData  = source{key: "claudeapi.data", ttl: dataTTL, service: enums.ServiceClaudeAPI, fetch: fetchClaudeAPI}
	ClaudePlanData = source{key: "claudeplan.data", ttl: opsTTL, service: enums.ServiceClaudePlan, fetch: fetchClaudePlan}
	OpenRouterData = source{key: "openrouter.data", ttl: dataTTL, service: enums.ServiceOpenRouter, fetch: fetchOpenRouter}
	OpenAIData     = source{key: "openai.data", ttl: dataTTL, service: enums.ServiceOpenAI, fetch: fetchOpenAI}
	DeepSeekData   = source{key: "deepseek.data", ttl: dataTTL, service: enums.ServiceDeepSeek, fetch: fetchDeepSeek}
	LiteLLMData    = source{key: "litellm.data", ttl: dataTTL, service: enums.ServiceLiteLLM, fetch: fetchLiteLLM}
)

// uses collects LLMUse rows by day and model, in first-seen order.
type uses struct {
	rows  []LLMUse
	index map[[2]string]int
}

// at is the row of a day and model, added when new.
func (u *uses) at(day, model string) *LLMUse {
	if u.index == nil {
		u.index = map[[2]string]int{}
	}
	k := [2]string{day, model}
	i, ok := u.index[k]
	if !ok {
		i = len(u.rows)
		u.index[k] = i
		u.rows = append(u.rows, LLMUse{Day: day, Model: model})
	}
	return &u.rows[i]
}

// bucketDay is the UTC day of a bucket's start, RFC 3339 or Unix seconds.
func bucketDay(v any) string {
	if s := asStr(v); s != "" {
		return parseTime(s).Format(time.DateOnly)
	}
	return time.Unix(asInt64(v), 0).UTC().Format(time.DateOnly)
}

// pages reads a cursor-paged report: data buckets until has_more is false.
func pages(ctx context.Context, api services.KeyedApi, path string, params url.Values) ([]any, error) {
	var buckets []any
	for range llmMaxPages {
		raw, err := api.Get(ctx, path, params)
		if err != nil {
			return nil, fetchError(err)
		}
		m := asMap(raw)
		buckets = append(buckets, asList(m["data"])...)
		next := asStr(m["next_page"])
		if !asBool(m["has_more"]) || next == "" {
			break
		}
		params.Set("page", next)
	}
	return buckets, nil
}

// ── Claude API (Admin key) ──

func fetchClaudeAPI(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoLLM(enums.ServiceClaudeAPI, time.Now()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.HeaderApi(sctx.URL, "x-api-key", secret, sctx.TLS())
	api.Headers["anthropic-version"] = anthropicVersion
	now := time.Now()
	from := historyStart(now).Format(time.RFC3339)
	limit := strconv.Itoa(llmDays)

	usage, err := pages(ctx, api, "v1/organizations/usage_report/messages",
		url.Values{"starting_at": {from}, "bucket_width": {"1d"}, "group_by[]": {"model"}, "limit": {limit}})
	if err != nil {
		return nil, err
	}
	costs, err := pages(ctx, api, "v1/organizations/cost_report", url.Values{"starting_at": {from}, "group_by[]": {"description"}, "limit": {limit}})
	if err != nil {
		return nil, err
	}

	var u uses
	for _, b := range usage {
		day := bucketDay(asMap(b)["starting_at"])
		for _, item := range asList(asMap(b)["results"]) {
			r := asMap(item)
			row := u.at(day, asStr(r["model"]))
			cache := asMap(r["cache_creation"])
			row.Input += asInt64(r["uncached_input_tokens"]) + asInt64(cache["ephemeral_5m_input_tokens"]) + asInt64(cache["ephemeral_1h_input_tokens"])
			row.Cached += asInt64(r["cache_read_input_tokens"])
			row.Output += asInt64(r["output_tokens"])
		}
	}
	for _, b := range costs {
		day := bucketDay(asMap(b)["starting_at"])
		for _, item := range asList(asMap(b)["results"]) {
			r := asMap(item)
			u.at(day, asStr(r["model"])).Cost += asFloat(r["amount"]) / centsPerDollar
		}
	}

	a := newAccount(enums.ServiceClaudeAPI, sctx, "")
	a.URL, a.Spend, a.Uses = "https://console.anthropic.com/usage", true, u.rows
	a.sumUses(now)
	return &LLMDataset{Accounts: []LLMAccount{a}}, nil
}

// ── Claude plan (Pro / Max, OAuth token) ──

// planWindows maps the flat answer's keys to quota windows.
var planWindows = []struct {
	key   string
	kind  QuotaKind
	model string
}{{"five_hour", QuotaSession, ""}, {"seven_day", QuotaWeek, ""}, {"seven_day_opus", QuotaWeekModel, "Opus"}, {"seven_day_sonnet", QuotaWeekModel, "Sonnet"}}

// planKinds maps the newer limits list's kinds.
var planKinds = map[string]QuotaKind{"session": QuotaSession, "weekly_all": QuotaWeek, "weekly_scoped": QuotaWeekModel}

func fetchClaudePlan(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoLLM(enums.ServiceClaudePlan, time.Now()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.BearerApi(sctx.URL, secret, sctx.TLS())
	api.Headers["anthropic-beta"] = oauthBeta
	raw, err := api.Get(ctx, "api/oauth/usage", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	a := newAccount(enums.ServiceClaudePlan, sctx, "")
	a.URL, a.Quotas = "https://claude.ai/settings/usage", planQuotas(asMap(raw))
	return &LLMDataset{Accounts: []LLMAccount{a}}, nil
}

// planQuotas reads the limits list when the answer has one, else the
// flat windows; null windows are left out.
func planQuotas(m map[string]any) []LLMQuota {
	var out []LLMQuota
	for _, item := range asList(m["limits"]) {
		l := asMap(item)
		kind, ok := planKinds[asStr(l["kind"])]
		if !ok {
			continue
		}
		model := asStr(asMap(asMap(l["scope"])["model"])["display_name"])
		out = append(out, LLMQuota{Kind: kind, Model: model, Percent: asFloat(l["percent"]), Resets: parseTime(l["resets_at"])})
	}
	if len(out) == 0 {
		for _, w := range planWindows {
			if win := asMap(m[w.key]); win != nil {
				out = append(out, LLMQuota{Kind: w.kind, Model: w.model, Percent: asFloat(win["utilization"]), Resets: parseTime(win["resets_at"])})
			}
		}
	}
	if extra := asMap(m["extra_usage"]); asBool(extra["is_enabled"]) {
		pct := asFloat(extra["utilization"])
		if limit := asFloat(extra["monthly_limit"]); extra["utilization"] == nil && limit > 0 {
			pct = asFloat(extra["used_credits"]) / limit * percent
		}
		out = append(out, LLMQuota{Kind: QuotaExtra, Percent: pct})
	}
	return out
}

// ── OpenRouter ──

func fetchOpenRouter(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoLLM(enums.ServiceOpenRouter, time.Now()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.BearerApi(sctx.URL, secret, sctx.TLS())
	raw, err := api.Get(ctx, "key", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	key := asMap(asMap(raw)["data"])
	a := newAccount(enums.ServiceOpenRouter, sctx, asStr(key["label"]))
	a.URL, a.Spend = "https://openrouter.ai/activity", true
	a.Today, a.Month = asFloat(key["usage_daily"]), asFloat(key["usage_monthly"])
	a.Limit, a.LimitLeft, a.LimitReset = asFloat(key["limit"]), asFloat(key["limit_remaining"]), asStr(key["limit_reset"])

	// Credit and history need a management key; an inference key stops here.
	if !asBool(key["is_management_key"]) && !asBool(key["is_provisioning_key"]) {
		return &LLMDataset{Accounts: []LLMAccount{a}}, nil
	}
	if raw, err := api.Get(ctx, "credits", nil); err == nil {
		c := asMap(asMap(raw)["data"])
		a.HasBalance, a.Balance, a.Currency = true, asFloat(c["total_credits"])-asFloat(c["total_usage"]), "USD"
	}
	if raw, err := api.Get(ctx, "activity", nil); err == nil {
		var u uses
		for _, item := range asList(asMap(raw)["data"]) {
			r := asMap(item)
			row := u.at(asStr(r["date"]), asStr(r["model"]))
			row.Cost += asFloat(r["usage"])
			row.Cached += asInt64(r["cached_tokens"])
			row.Input += asInt64(r["prompt_tokens"]) - asInt64(r["cached_tokens"])
			row.Output += asInt64(r["completion_tokens"])
			row.Requests += asInt64(r["requests"])
		}
		a.Uses = u.rows
	}
	return &LLMDataset{Accounts: []LLMAccount{a}}, nil
}

// ── OpenAI (Admin key) ──

func fetchOpenAI(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoLLM(enums.ServiceOpenAI, time.Now()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.BearerApi(sctx.URL, secret, sctx.TLS())
	now := time.Now()
	from := strconv.FormatInt(historyStart(now).Unix(), 10)
	limit := strconv.Itoa(llmDays)

	usage, err := pages(ctx, api, "organization/usage/completions", url.Values{"start_time": {from}, "bucket_width": {"1d"}, "group_by": {"model"}, "limit": {limit}})
	if err != nil {
		return nil, err
	}
	costs, err := pages(ctx, api, "organization/costs", url.Values{"start_time": {from}, "bucket_width": {"1d"}, "group_by": {"line_item"}, "limit": {limit}})
	if err != nil {
		return nil, err
	}

	var u uses
	for _, b := range usage {
		day := bucketDay(asMap(b)["start_time"])
		for _, item := range asList(asMap(b)["results"]) {
			r := asMap(item)
			row := u.at(day, asStr(r["model"]))
			cached := asInt64(r["input_cached_tokens"])
			row.Input += asInt64(r["input_tokens"]) - cached
			row.Cached += cached
			row.Output += asInt64(r["output_tokens"])
			row.Requests += asInt64(r["num_model_requests"])
		}
	}
	for _, b := range costs {
		day := bucketDay(asMap(b)["start_time"])
		for _, item := range asList(asMap(b)["results"]) {
			r := asMap(item)
			u.at(day, lineModel(asStr(r["line_item"]))).Cost += asFloat(asMap(r["amount"])["value"])
		}
	}

	a := newAccount(enums.ServiceOpenAI, sctx, "")
	a.URL, a.Spend, a.Uses = "https://platform.openai.com/usage", true, u.rows
	a.sumUses(now)
	return &LLMDataset{Accounts: []LLMAccount{a}}, nil
}

// lineModel is the model of a cost line item: "gpt-5-mini, input" → "gpt-5-mini".
func lineModel(item string) string {
	model, _, _ := strings.Cut(item, ",")
	return strings.TrimSpace(model)
}

// ── DeepSeek ──

func fetchDeepSeek(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoLLM(enums.ServiceDeepSeek, time.Now()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	raw, err := services.BearerApi(sctx.URL, secret, sctx.TLS()).Get(ctx, "user/balance", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	a := newAccount(enums.ServiceDeepSeek, sctx, "")
	a.URL = "https://platform.deepseek.com/usage"

	// One balance per currency; USD when there is one, else the first.
	for _, item := range asList(asMap(raw)["balance_infos"]) {
		b := asMap(item)
		if a.HasBalance && asStr(b["currency"]) != "USD" {
			continue
		}
		a.HasBalance, a.Balance, a.Currency = true, asFloat(b["total_balance"]), asStr(b["currency"])
	}
	return &LLMDataset{Accounts: []LLMAccount{a}}, nil
}

// ── LiteLLM proxy ──

func fetchLiteLLM(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoLLM(enums.ServiceLiteLLM, time.Now()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.BearerApi(sctx.URL, secret, sctx.TLS())
	now := time.Now()
	params := url.Values{"start_date": {historyStart(now).Format(time.DateOnly)}, "end_date": {now.UTC().Format(time.DateOnly)},
		"page_size": {strconv.Itoa(litellmPageSize)}}

	var u uses
	for page := 1; page <= llmMaxPages; page++ {
		params.Set("page", strconv.Itoa(page))
		raw, err := api.Get(ctx, "user/daily/activity", params)
		if err != nil {
			return nil, fetchError(err)
		}
		m := asMap(raw)
		for _, item := range asList(m["results"]) {
			litellmDay(&u, asMap(item))
		}
		if page >= int(asInt64(asMap(m["metadata"])["total_pages"])) {
			break
		}
	}

	a := newAccount(enums.ServiceLiteLLM, sctx, "")
	a.URL, a.Spend, a.Uses = strings.TrimRight(sctx.URL, "/")+"/ui", true, u.rows
	a.sumUses(now)
	return &LLMDataset{Accounts: []LLMAccount{a}}, nil
}

// litellmDay adds one day's models; a model entry holds its numbers under
// "metrics" (newer proxies) or directly.
func litellmDay(u *uses, day map[string]any) {
	date := asStr(day["date"])
	models := asMap(asMap(day["breakdown"])["models"])
	for _, model := range slices.Sorted(maps.Keys(models)) {
		m := asMap(models[model])
		if inner := asMap(m["metrics"]); inner != nil {
			m = inner
		}
		row := u.at(date, model)
		cached := asInt64(m["cache_read_input_tokens"])
		row.Cost += asFloat(m["spend"])
		row.Input += asInt64(m["prompt_tokens"]) - cached
		row.Cached += cached
		row.Output += asInt64(m["completion_tokens"])
		row.Requests += asInt64(m["api_requests"])
	}
}
