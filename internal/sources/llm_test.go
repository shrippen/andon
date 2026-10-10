package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

// fakeLLM answers paths with fixed JSON when the request carries the
// header; anything else is 401. A key in the query fails the test.
func fakeLLM(t *testing.T, header, value string, answers map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.RawQuery, "secret") {
			t.Errorf("key in query: %s", r.URL)
		}
		body, ok := answers[r.URL.Path]
		if !ok || r.Header.Get(header) != value {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// account fetches one source and returns its single account.
func account(t *testing.T, src sources.Source, url string, options map[string]any) sources.LLMAccount {
	t.Helper()
	raw, err := src.Fetch(context.Background(), sources.Ctx{URL: url, Secret: "secret", Options: options})
	if err != nil {
		t.Fatal(err)
	}
	data := raw.(*sources.LLMDataset)
	if len(data.Accounts) != 1 {
		t.Fatalf("accounts %+v", data.Accounts)
	}
	return data.Accounts[0]
}

// TestClaudeAPI: tokens from the usage report and cents from the cost
// report meet per day and model; cache writes count as input; the
// budget comes from the connection.
func TestClaudeAPI(t *testing.T) {
	today := time.Now().UTC().Format(time.DateOnly)
	srv := fakeLLM(t, "x-api-key", "secret", map[string]string{
		"/v1/organizations/usage_report/messages": `{"data":[{"starting_at":"` + today + `T00:00:00Z","results":[{"model":"claude-opus-5-5",` +
			`"uncached_input_tokens":1000,"cache_creation":{"ephemeral_5m_input_tokens":200,"ephemeral_1h_input_tokens":0},"cache_read_input_tokens":300,"output_tokens":50}]}],"has_more":false}`,
		"/v1/organizations/cost_report": `{"data":[{"starting_at":"` + today + `T00:00:00Z","results":[{"model":"claude-opus-5-5","amount":"150.5","currency":"USD"},` +
			`{"model":"claude-opus-5-5","amount":"49.5","currency":"USD"},{"model":null,"amount":"100","currency":"USD","cost_type":"web_search"}]}],"has_more":false}`,
	})
	a := account(t, sources.ClaudeAPIData, srv.URL, map[string]any{"budget": "60"})
	if len(a.Uses) != 2 || a.Uses[0].Input != 1200 || a.Uses[0].Cached != 300 || a.Uses[0].Cost != 2 || a.Uses[1].Model != "" {
		t.Fatalf("uses %+v", a.Uses)
	}
	if !a.Spend || a.Today != 3 || a.Month != 3 || a.Budget != 60 {
		t.Fatalf("account %+v", a)
	}
}

// TestClaudePlan: the newer limits list wins; without it the flat
// windows, null ones left out; extra usage as a share of its limit.
func TestClaudePlan(t *testing.T) {
	srv := fakeLLM(t, "Authorization", "Bearer secret", map[string]string{
		"/api/oauth/usage": `{"limits":[{"kind":"session","percent":12,"resets_at":"2026-10-10T15:00:00+00:00"},` +
			`{"kind":"weekly_scoped","percent":40,"scope":{"model":{"display_name":"Opus"}}},{"kind":"odd","percent":99}],"five_hour":{"utilization":1}}`,
	})
	a := account(t, sources.ClaudePlanData, srv.URL, nil)
	if len(a.Quotas) != 2 || a.Quotas[0].Kind != sources.QuotaSession || a.Quotas[0].Resets.IsZero() || a.Quotas[1].Model != "Opus" || a.Spend {
		t.Fatalf("limits %+v", a)
	}

	srv = fakeLLM(t, "Authorization", "Bearer secret", map[string]string{
		"/api/oauth/usage": `{"five_hour":{"utilization":35.0,"resets_at":"2026-10-10T15:00:00+00:00"},"seven_day":{"utilization":91},"seven_day_opus":null,` +
			`"extra_usage":{"is_enabled":true,"monthly_limit":5000,"used_credits":1250,"utilization":null}}`,
	})
	a = account(t, sources.ClaudePlanData, srv.URL, nil)
	if len(a.Quotas) != 3 || a.Quotas[1].Kind != sources.QuotaWeek || a.Quotas[1].Percent != 91 || a.Quotas[2].Kind != sources.QuotaExtra || a.Quotas[2].Percent != 25 {
		t.Fatalf("flat %+v", a.Quotas)
	}
}

// TestOpenRouter: an inference key gives spend and its limit; a
// management key also credit and the history per model.
func TestOpenRouter(t *testing.T) {
	key := `{"data":{"label":"subs","usage_daily":1.5,"usage_monthly":20,"limit":25,"limit_remaining":5,"limit_reset":"monthly","is_management_key":false}}`
	srv := fakeLLM(t, "Authorization", "Bearer secret", map[string]string{"/key": key})
	a := account(t, sources.OpenRouterData, srv.URL, nil)
	if a.Name != "subs" || a.Today != 1.5 || a.Month != 20 || a.Limit != 25 || a.LimitLeft != 5 || a.HasBalance || len(a.Uses) != 0 {
		t.Fatalf("inference key %+v", a)
	}

	srv = fakeLLM(t, "Authorization", "Bearer secret", map[string]string{
		"/key":     strings.Replace(key, `"is_management_key":false`, `"is_management_key":true`, 1),
		"/credits": `{"data":{"total_credits":40,"total_usage":36.2}}`,
		"/activity": `{"data":[{"date":"2026-10-09","model":"google/gemini-2.5-flash","usage":0.4,"requests":3,"prompt_tokens":1000,"cached_tokens":100,"completion_tokens":200},` +
			`{"date":"2026-10-09","model":"google/gemini-2.5-flash","usage":0.1,"requests":1,"prompt_tokens":10,"completion_tokens":2}]}`,
	})
	a = account(t, sources.OpenRouterData, srv.URL, nil)
	if !a.HasBalance || a.Balance < 3.79 || a.Balance > 3.81 || len(a.Uses) != 1 || a.Uses[0].Input != 910 || a.Uses[0].Requests != 4 || a.Uses[0].Cost != 0.5 {
		t.Fatalf("management key %+v", a)
	}
}

// TestOpenAI: tokens per model, cost per line item's model; buckets in
// Unix seconds.
func TestOpenAI(t *testing.T) {
	start := time.Now().UTC().Truncate(24 * time.Hour).Unix()
	at := `"start_time":` + strconv.FormatInt(start, 10)
	srv := fakeLLM(t, "Authorization", "Bearer secret", map[string]string{
		"/organization/usage/completions": `{"data":[{` + at + `,"results":[{"model":"gpt-5-mini","input_tokens":500,"input_cached_tokens":100,"output_tokens":40,"num_model_requests":7}]}],"has_more":false}`,
		"/organization/costs":             `{"data":[{` + at + `,"results":[{"amount":{"value":0.25,"currency":"usd"},"line_item":"gpt-5-mini, input"},{"amount":{"value":0.5,"currency":"usd"},"line_item":"gpt-5-mini, output"}]}],"has_more":false}`,
	})
	a := account(t, sources.OpenAIData, srv.URL, nil)
	if len(a.Uses) != 1 || a.Uses[0].Input != 400 || a.Uses[0].Cached != 100 || a.Uses[0].Requests != 7 || a.Uses[0].Cost != 0.75 || a.Today != 0.75 {
		t.Fatalf("openai %+v", a)
	}
}

// TestDeepSeek: the USD balance when there is one.
func TestDeepSeek(t *testing.T) {
	srv := fakeLLM(t, "Authorization", "Bearer secret", map[string]string{
		"/user/balance": `{"is_available":true,"balance_infos":[{"currency":"CNY","total_balance":"110.00"},{"currency":"USD","total_balance":"2.40"}]}`,
	})
	a := account(t, sources.DeepSeekData, srv.URL, nil)
	if !a.HasBalance || a.Balance != 2.4 || a.Currency != "USD" || a.Spend {
		t.Fatalf("deepseek %+v", a)
	}
}

// TestLiteLLM: models of each day, numbers under "metrics" or directly;
// pages until total_pages.
func TestLiteLLM(t *testing.T) {
	today := time.Now().UTC().Format(time.DateOnly)
	srv := fakeLLM(t, "Authorization", "Bearer secret", map[string]string{
		"/user/daily/activity": `{"results":[{"date":"` + today + `","breakdown":{"models":{` +
			`"gpt-4o-mini":{"metrics":{"spend":0.2,"prompt_tokens":100,"completion_tokens":20,"api_requests":2}},` +
			`"ollama/qwen3":{"spend":0,"prompt_tokens":900,"completion_tokens":90,"api_requests":9}}}}],"metadata":{"total_pages":1}}`,
	})
	a := account(t, sources.LiteLLMData, srv.URL, nil)
	if len(a.Uses) != 2 || a.Uses[0].Model != "gpt-4o-mini" || a.Uses[1].Input != 900 || a.Today != 0.2 {
		t.Fatalf("litellm %+v", a)
	}
}

// TestDemoLLM: every provider has Studio Weber's account; the API one
// spends today, the plan has quotas, DeepSeek only a balance.
func TestDemoLLM(t *testing.T) {
	now := time.Now()
	for _, s := range sources.LLMServices() {
		if d := sources.DemoLLM(s, now); len(d.Accounts) != 1 || d.Accounts[0].Service != s {
			t.Fatalf("%s: %+v", s, d)
		}
	}
	api := sources.DemoLLM(enums.ServiceClaudeAPI, now).Accounts[0]
	if api.Today <= 0 || api.Budget == 0 || len(api.Uses) == 0 || api.Uses[len(api.Uses)-1].Day != now.UTC().Format(time.DateOnly) {
		t.Fatalf("claude api %+v", api)
	}
	if plan := sources.DemoLLM(enums.ServiceClaudePlan, now).Accounts[0]; len(plan.Quotas) < 3 || plan.Quotas[0].Resets.IsZero() {
		t.Fatalf("plan %+v", plan)
	}
	if ds := sources.DemoLLM(enums.ServiceDeepSeek, now).Accounts[0]; !ds.HasBalance || ds.Spend {
		t.Fatalf("deepseek %+v", ds)
	}
}
