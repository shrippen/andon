package sources

import (
	"andon/internal/caps"
	"context"
	"net/url"
	"strconv"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
)

const (
	surePage     = 100
	sureMaxPages = 10
	sureDays     = 90
	centsPerUnit = 100
)

type SureAccount struct {
	ID, Name, Type string // Type: depository, credit_card, investment, …
	Classification string // asset or liability
	Balance        float64
	Currency       string
	Role           AccountRole
}

// SureTxn is one booking; Amount is signed: income > 0, expense < 0.
type SureTxn struct {
	ID, Date, Name     string
	Amount             float64
	Category, Merchant string
	Account            string
	Tags               []string // Sure's tags, e.g. set by its rules

	// The account's id and role, for business or private.
	AccountID string
	Role      AccountRole

	// CategoryID is Category's id ("" none); Transfer marks a booking
	// between own accounts, which needs no category.
	CategoryID string
	Transfer   bool
}

// SureCategory is one of Sure's categories; Parent names the one it
// belongs to ("" top level).
type SureCategory struct {
	ID, Name, Parent string
}

// SureRecurring is an expected payment; Amount is positive, Expense tells
// the direction (Sure stores outflows as positive amounts).
type SureRecurring struct {
	Name, Status  string // active, inactive
	Amount        float64
	Expense       bool
	Next, Last    string // "2026-10-01"
	Min, Max, Avg float64
}

type SureDataset struct {
	URL          string
	Currency     string
	NetWorth     float64
	Accounts     []SureAccount
	Transactions []SureTxn // last 90 days
	Recurring    []SureRecurring
	Categories   []SureCategory // Sure only; Firefly leaves them out
	SyncError    string         // latest sync failed: its message
	// Service is where the data comes from: "" Sure, or Firefly III,
	// which fills the same shape (firefly.go).
	Service enums.ServiceType
}

// From is the service the bank data comes from.
func (d *SureDataset) From() enums.ServiceType {
	if d.Service == "" {
		return enums.ServiceSure
	}
	return d.Service
}

// CapSet: the capabilities need nothing beyond the token.
func (d *SureDataset) CapSet() caps.Set { return caps.Full(caps.HolderOf(d.From())) }

var SureData = source{key: "sure.data", ttl: dataTTL, service: enums.ServiceSure, fetch: fetchSure}

func fetchSure(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		data := DemoSure(time.Now())
		data.setRoles(accountRoles(sctx.Options))
		return data, nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	data, err := loadSure(ctx, services.SureApi{URL: sctx.URL, Key: secret, Verify: sctx.VerifyTLS}, sctx.URL, time.Now())
	if err != nil {
		return nil, fetchError(err)
	}
	data.setRoles(accountRoles(sctx.Options))
	return data, nil
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

// cents reads *_cents fields as currency units.
func cents(v any) float64 { return asFloat(v) / centsPerUnit }

// moneyOf reads a Money#as_json value ({"amount": "12.5", "currency": …})
// or a plain number.
func moneyOf(v any) float64 {
	if m, ok := v.(map[string]any); ok {
		return asFloat(m["amount"])
	}
	return asFloat(v)
}

func loadSure(ctx context.Context, api services.SureApi, base string, now time.Time) (*SureDataset, error) {
	sheet, err := api.Get(ctx, "balance_sheet", nil)
	if err != nil {
		return nil, err
	}
	data := &SureDataset{URL: base, Currency: asStr(asMap(sheet)["currency"]), NetWorth: moneyOf(asMap(sheet)["net_worth"])}

	accounts, err := api.Get(ctx, "accounts", url.Values{"per_page": {strconv.Itoa(surePage)}})
	if err != nil {
		return nil, err
	}
	for _, raw := range asList(asMap(accounts)["accounts"]) {
		a := asMap(raw)
		data.Accounts = append(data.Accounts, SureAccount{
			ID: asStr(a["id"]), Name: asStr(a["name"]), Type: asStr(a["account_type"]),
			Classification: asStr(a["classification"]), Balance: cents(a["balance_cents"]), Currency: asStr(a["currency"]),
		})
	}

	since := now.AddDate(0, 0, -sureDays).Format(time.DateOnly)
	for page := 1; page <= sureMaxPages; page++ {
		txns, err := api.Get(ctx, "transactions", url.Values{"start_date": {since}, "per_page": {strconv.Itoa(surePage)}, "page": {strconv.Itoa(page)}})
		if err != nil {
			return nil, err
		}
		list := asList(asMap(txns)["transactions"])
		for _, raw := range list {
			t := asMap(raw)
			data.Transactions = append(data.Transactions, SureTxn{
				ID: asStr(t["id"]), Date: asStr(t["date"]), Name: cleanName(asStr(t["name"])), Amount: cents(t["signed_amount_cents"]),
				Category: asStr(asMap(t["category"])["name"]), Merchant: cleanName(asStr(asMap(t["merchant"])["name"])),
				Account: asStr(asMap(t["account"])["name"]), Tags: sureTags(t["tags"]), AccountID: asStr(asMap(t["account"])["id"]),
				CategoryID: asStr(asMap(t["category"])["id"]), Transfer: t["transfer"] != nil,
			})
		}
		if len(list) < surePage {
			break
		}
	}

	data.Categories = sureCategories(ctx, api)

	// Recurring detection and sync state are newer API parts; older
	// servers simply lack them.
	if rec, err := api.Get(ctx, "recurring_transactions", url.Values{"per_page": {strconv.Itoa(surePage)}}); err == nil {
		list := asList(asMap(rec)["recurring_transactions"])
		if list == nil {
			list = asList(rec)
		}
		for _, raw := range list {
			r := asMap(raw)
			// Detected from a merchant, a recurring item has no name of
			// its own; Sure then shows the merchant's.
			name := cleanName(asStr(r["name"]))
			if name == "" {
				name = cleanName(asStr(asMap(r["merchant"])["name"]))
			}
			data.Recurring = append(data.Recurring, SureRecurring{
				Name: name, Status: asStr(r["status"]), Amount: abs(cents(r["amount_cents"])), Expense: cents(r["amount_cents"]) > 0,
				Next: asStr(r["next_expected_date"]), Last: asStr(r["last_occurrence_date"]),
				Min: cents(r["expected_amount_min_cents"]), Max: cents(r["expected_amount_max_cents"]), Avg: cents(r["expected_amount_avg_cents"]),
			})
		}
	}
	if sync, err := api.Get(ctx, "syncs/latest", nil); err == nil {
		s := asMap(sync)
		if d, ok := s["data"].(map[string]any); ok {
			s = d
		}
		if asStr(s["status"]) == "failed" {
			data.SyncError = asStr(asMap(s["syncable"])["name"])
		}
	}
	return data, nil
}

// setRoles marks each account and its transactions with the role the
// user named; a booking without account id finds it by name (demo).
func (d *SureDataset) setRoles(roles map[string]AccountRole) {
	byName := map[string]string{}
	for i := range d.Accounts {
		a := &d.Accounts[i]
		a.Role = roles[a.ID]
		byName[a.Name] = a.ID
	}
	for i := range d.Transactions {
		t := &d.Transactions[i]
		if t.AccountID == "" {
			t.AccountID = byName[t.Account]
		}
		t.Role = roles[t.AccountID]
	}
}

// Only is the dataset with the accounts of one role and their bookings;
// without a role asked for, or none named yet, all of it. Recurring
// payments name no account and stay.
func (d *SureDataset) Only(role AccountRole) *SureDataset {
	if role == RoleUnknown || !d.Classified() {
		return d
	}
	out := *d
	out.Accounts, out.Transactions = nil, nil
	for _, a := range d.Accounts {
		if a.Role == role {
			out.Accounts = append(out.Accounts, a)
		}
	}
	for _, t := range d.Transactions {
		if t.Role == role {
			out.Transactions = append(out.Transactions, t)
		}
	}
	return &out
}

// Classified says whether any account has a role.
func (d *SureDataset) Classified() bool {
	for _, a := range d.Accounts {
		if a.Role != RoleUnknown {
			return true
		}
	}
	return false
}

// sureTags reads the names of a transaction's tags.
func sureTags(raw any) []string {
	var out []string
	for _, t := range asList(raw) {
		if name := asStr(asMap(t)["name"]); name != "" {
			out = append(out, name)
		}
	}
	return out
}

func init() {
	Register(SureData)
	Register(testOf{SureData, func(d any) map[string]any { return map[string]any{"accounts": len(d.(*SureDataset).Accounts)} }})
}

// sureCategories reads every category; a server without the endpoint
// has none for Andon.
func sureCategories(ctx context.Context, api services.SureApi) []SureCategory {
	var out []SureCategory
	for page := 1; page <= sureMaxPages; page++ {
		raw, err := api.Get(ctx, "categories", url.Values{"per_page": {strconv.Itoa(surePage)}, "page": {strconv.Itoa(page)}})
		if err != nil {
			return out
		}
		list := asList(asMap(raw)["categories"])
		for _, c := range list {
			m := asMap(c)
			out = append(out, SureCategory{ID: asStr(m["id"]), Name: asStr(m["name"]), Parent: asStr(asMap(m["parent"])["name"])})
		}
		if len(list) < surePage {
			return out
		}
	}
	return out
}
