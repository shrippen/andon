package rules_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/rules"
	"andon/internal/sources"
)

// TestPaymentsFromFirefly: the payment checks read Firefly III's bank
// data as they read Sure's, and its own rules run under firefly ids.
func TestPaymentsFromFirefly(t *testing.T) {
	ninja := &sources.NinjaDataset{Currency: "EUR", Clients: []sources.NinjaClient{{ID: 1, Name: "Acme GmbH"}},
		Invoices: []sources.NinjaInvoice{{ID: 7, Number: "RE-2026-041", ClientID: 1, Status: "sent", Date: "2026-09-01", Amount: 1190, Balance: 1190}}}
	ff := &sources.SureDataset{Service: enums.ServiceFirefly, Currency: "EUR",
		Transactions: []sources.SureTxn{{ID: "a", Date: "2026-09-20", Name: "Acme GmbH RE 2026 041", Amount: 1190}},
		Accounts:     []sources.SureAccount{{ID: "g", Name: "Giro", Type: "depository", Classification: "asset", Balance: 50}}}
	env := crossEnv("2026-09-25", map[string]any{"firefly": ff, "invoiceninja": ninja})
	if got := run(t, "cross.invoice_paid", nil, env); len(got) != 1 {
		t.Fatalf("paid via firefly: %+v", got)
	}
	low := run(t, "firefly.low_balance", ff, env)
	if len(low) != 1 || low[0].Rule != "firefly.low_balance" || low[0].Sources[0] != "firefly" {
		t.Fatalf("low balance: %+v", low)
	}
}

// TestDepotRules: a month down by more than the limit is a warning; a cash
// gap the depot could close is a note.
func TestDepotRules(t *testing.T) {
	g := &sources.GhostfolioDataset{Currency: "EUR", Value: 20000, PerformancePct: -12}
	if got := run(t, "ghostfolio.drawdown", g, todayEnv(nil)); len(got) != 1 {
		t.Fatalf("drawdown: %+v", got)
	}
	g.PerformancePct = 4
	if got := run(t, "ghostfolio.drawdown", g, todayEnv(nil)); len(got) != 0 {
		t.Fatalf("up: %+v", got)
	}

	today := time.Now().UTC()
	sure := &sources.SureDataset{Currency: "EUR", Accounts: []sources.SureAccount{{ID: "g", Classification: "asset", Balance: 100}},
		Recurring: []sources.SureRecurring{{Name: "Miete", Status: "active", Amount: 1500, Expense: true, Next: today.AddDate(0, 0, 5).Format(time.DateOnly)}}}
	ninja := &sources.NinjaDataset{Currency: "EUR"}
	env := todayEnv(nil)
	env.Today = today
	env.Datasets = map[string]any{"sure": sure, "invoiceninja": ninja, "ghostfolio": g}
	if got := run(t, "cross.depot_reserve", nil, env); len(got) != 1 {
		t.Fatalf("reserve: %+v", got)
	}
}

// A quote counts as open once Invoice Ninja's validity has passed; one
// without validity after the rule's days.
func TestQuoteOpenValidity(t *testing.T) {
	data := &sources.NinjaDataset{Quotes: []sources.NinjaQuote{
		{ID: 1, Status: "sent", Date: "2026-09-01", ValidUntil: "2026-10-20"}, // still valid
		{ID: 2, Status: "sent", Date: "2026-10-01", ValidUntil: "2026-10-10"}, // expired
		{ID: 3, Status: "sent", Date: "2026-09-01"},                           // 43 days, no validity
	}}
	env := rules.Env{Today: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)}
	got := map[string]bool{}
	for _, f := range run(t, "in.quote_open", data, env) {
		got[f.Fingerprint] = true
	}
	if len(got) != 2 || !got["quote:2"] || !got["quote:3"] {
		t.Errorf("quotes: %v", got)
	}
}
