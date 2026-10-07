package sources

// Firefly III fills the same bank shape as Sure (SureDataset with
// Service = firefly), so payment checks, subscriptions and spendable
// cash read either. A personal access token is enough.
//
//	GET api/v1/accounts?type=asset                    → data[{id, attributes{name, current_balance, currency_code}}]
//	GET api/v1/transactions?start=…&end=…&page=N      → data[{id, attributes{transactions[{type, date, amount, description, source_name, destination_name, category_name}]}}]
//	GET api/v1/recurrences                            → data[{attributes{title, active, type, transactions[{amount}], repetitions[{occurrences[]}]}}]
//	GET api/v1/summary/basic?start=…&end=…            → {"net-worth-in-EUR": {monetary_value}}

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
)

const (
	fireflyMaxPages   = 10
	fireflyDeposit    = "deposit"
	fireflyWithdrawal = "withdrawal"
	fireflyNetWorth   = "net-worth-in-"
)

var FireflyData = source{key: "firefly.data", ttl: dataTTL, service: enums.ServiceFirefly, fetch: fetchFirefly}

func fetchFirefly(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoFirefly(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.BearerApi(sctx.URL, secret, sctx.TLS())
	data := &SureDataset{URL: sctx.URL, Service: enums.ServiceFirefly}
	today := time.Now().UTC()
	start, end := today.AddDate(0, 0, -sureDays).Format(time.DateOnly), today.Format(time.DateOnly)

	accounts, err := api.Get(ctx, "api/v1/accounts", url.Values{"type": {"asset"}})
	if err != nil {
		return nil, fetchError(err)
	}
	for _, raw := range asList(asMap(accounts)["data"]) {
		a := asMap(raw)
		attr := asMap(a["attributes"])
		data.Currency = asStr(attr["currency_code"])
		data.Accounts = append(data.Accounts, SureAccount{ID: asStr(a["id"]), Name: asStr(attr["name"]), Type: "depository",
			Classification: "asset", Balance: asFloat(attr["current_balance"]), Currency: data.Currency})
	}

	for page := 1; page <= fireflyMaxPages; page++ {
		list, err := api.Get(ctx, "api/v1/transactions", url.Values{"start": {start}, "end": {end}, "page": {strconv.Itoa(page)}})
		if err != nil {
			return nil, fetchError(err)
		}
		for _, raw := range asList(asMap(list)["data"]) {
			group := asMap(raw)
			for _, item := range asList(asMap(group["attributes"])["transactions"]) {
				if t, ok := fireflyTxn(asStr(group["id"]), asMap(item)); ok {
					data.Transactions = append(data.Transactions, t)
				}
			}
		}
		pages := asMap(asMap(asMap(list)["meta"])["pagination"])
		if page >= int(asFloat(pages["total_pages"])) {
			break
		}
	}

	if rec, err := api.Get(ctx, "api/v1/recurrences", nil); err == nil {
		for _, raw := range asList(asMap(rec)["data"]) {
			data.Recurring = append(data.Recurring, fireflyRecurring(asMap(asMap(raw)["attributes"]), today))
		}
	}
	if sum, err := api.Get(ctx, "api/v1/summary/basic", url.Values{"start": {start}, "end": {end}}); err == nil {
		for key, v := range asMap(sum) {
			if strings.HasPrefix(key, fireflyNetWorth) {
				data.NetWorth += asFloat(asMap(v)["monetary_value"])
			}
		}
	}
	return data, nil
}

// fireflyTxn turns a split into Sure's shape: deposits positive with the
// payer as merchant, withdrawals negative; transfers stay out.
func fireflyTxn(id string, t map[string]any) (SureTxn, bool) {
	amount := asFloat(t["amount"])
	out := SureTxn{ID: id, Date: asStr(t["date"])[:min(len(asStr(t["date"])), len(time.DateOnly))], Name: asStr(t["description"]), Category: asStr(t["category_name"])}
	switch asStr(t["type"]) {
	case fireflyDeposit:
		out.Amount, out.Merchant, out.Account = amount, asStr(t["source_name"]), asStr(t["destination_name"])
	case fireflyWithdrawal:
		out.Amount, out.Merchant, out.Account = -amount, asStr(t["destination_name"]), asStr(t["source_name"])
	default:
		return SureTxn{}, false
	}
	return out, true
}

// fireflyRecurring takes a recurrence: its amount, the next occurrence
// from today on.
func fireflyRecurring(a map[string]any, today time.Time) SureRecurring {
	r := SureRecurring{Name: asStr(a["title"]), Status: "inactive", Expense: asStr(a["type"]) == fireflyWithdrawal}
	if active, _ := a["active"].(bool); active {
		r.Status = "active"
	}
	for _, t := range asList(a["transactions"]) {
		r.Amount = asFloat(asMap(t)["amount"])
		break
	}
	r.Avg, r.Min, r.Max = r.Amount, r.Amount, r.Amount
	for _, rep := range asList(a["repetitions"]) {
		for _, occ := range asList(asMap(rep)["occurrences"]) {
			day := asStr(occ)[:min(len(asStr(occ)), len(time.DateOnly))]
			if day >= today.Format(time.DateOnly) && (r.Next == "" || day < r.Next) {
				r.Next = day
			}
		}
	}
	return r
}

// DemoFirefly is the studio's bank data as Firefly III would report it.
func DemoFirefly(now time.Time) *SureDataset {
	data := DemoSure(now)
	data.Service, data.URL = enums.ServiceFirefly, "https://firefly.demo"
	return data
}

func init() {
	Register(FireflyData)
	Register(testOf{FireflyData, func(d any) map[string]any { return map[string]any{"transactions": len(d.(*SureDataset).Transactions)} }})
}
