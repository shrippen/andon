package metrics

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// subActions are the cases' actions by key.
func subActions(cases []SubCase) map[string]SubAction {
	out := map[string]SubAction{}
	for _, c := range cases {
		out[c.Key] = c.Action
	}
	return out
}

func TestSubCasesDemo(t *testing.T) {
	now := time.Now()
	// The business account's role, as the demo connection sets it.
	bank := sources.DemoSure(now)
	for i, t := range bank.Transactions {
		if t.AccountID == "a1" || t.Account == bank.Accounts[0].Name {
			bank.Transactions[i].Role = sources.RoleBusiness
		}
	}
	cases := SubCases(SubInputs{Bank: bank, Wallos: sources.DemoWallos(now), Ninja: sources.DemoNinja(now), Today: now})
	want := map[string]SubAction{"wp-1": SubWallosPrice, "wm-5": SubWallosMerge, "wa-farbraum-grade-abo": SubWallosAdd, "np-demo-r1": SubNinjaPrice, "na-farbraum-grade-abo": SubNinjaAdd}
	got := subActions(cases)
	for k, a := range want {
		if got[k] != a {
			t.Errorf("case %s = %q, want %q (all: %v)", k, got[k], a, got)
		}
	}
	for _, c := range cases {
		if c.Key == "np-demo-r1" && (c.Old != 41.65 || c.Amount != 44.9 || !c.Ticked) {
			t.Errorf("price case %+v", c)
		}
	}
}

// One payment matching two subscriptions ("Netkom" → Internet and
// phone) proposes a price only for the nearer one, not ticked; an
// ignored name is not added to Wallos, nor a variable Ninja bill
// repriced.
func TestSubCasesAmbiguous(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.Local)
	bank := &sources.SureDataset{Recurring: []sources.SureRecurring{
		{Name: "Netkom", Status: "active", Amount: 51.40, Expense: true},
		{Name: "DKB AG", Status: "active", Amount: 2.50, Expense: true},
		{Name: "Stadtwerke", Status: "active", Amount: 80, Expense: true},
	}}
	wallos := &sources.WallosDataset{Main: 1, Subs: []sources.WallosSub{
		{ID: 1, Name: "Netkom Internet", Price: 54.95, CurrencyID: 1, Cycle: 3, Frequency: 1},
		{ID: 2, Name: "Netkom Handy", Price: 15, CurrencyID: 1, Cycle: 3, Frequency: 1},
		{ID: 3, Name: "Stadtwerke", Price: 80, CurrencyID: 1, Cycle: 3, Frequency: 1},
	}}
	ninja := &sources.NinjaDataset{Vendors: []sources.NinjaVendor{{Key: "v1", Name: "Stadtwerke"}},
		RecurringExpenses: []sources.NinjaRecurringExpense{{Key: "r1", VendorKey: "v1", Amount: 0, Months: 1, Active: true}}}

	cases := SubCases(SubInputs{Bank: bank, Wallos: wallos, Ninja: ninja, Today: now, Ignore: []string{"dkb"}})
	if len(cases) != 1 {
		t.Fatalf("cases: %+v", cases)
	}
	if c := cases[0]; c.Key != "wp-1" || c.Ticked || c.Amount != 51.40 {
		t.Errorf("price case: %+v", c)
	}
}
