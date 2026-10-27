package sources

import "testing"

// An ignored account leaves Accounts and takes its bookings along; the
// role pages still find it under Ignored. Invoice Ninja's feed drops
// the transactions of an ignored account.
func TestIgnoredAccounts(t *testing.T) {
	bank := &BankData{
		Accounts:     []BankAccount{{ID: "giro", Name: "Giro"}, {ID: "depot", Name: "Depot", Type: "investment"}},
		Transactions: []BankTxn{{ID: "t1", AccountID: "giro"}, {ID: "t2", AccountID: "depot"}, {ID: "t3", Account: "Depot"}},
	}
	roles := accountRoles(map[string]any{OptAccountRoles: map[string]any{"giro": "business", "depot": "ignored", "x": "junk"}})
	if len(roles) != 2 {
		t.Fatalf("roles: %v", roles)
	}
	bank.setRoles(roles)
	if len(bank.Accounts) != 1 || bank.Accounts[0].ID != "giro" || len(bank.Ignored) != 1 || bank.Ignored[0].Role != RoleIgnored {
		t.Errorf("accounts %+v, ignored %+v", bank.Accounts, bank.Ignored)
	}
	if len(bank.Transactions) != 1 || bank.Transactions[0].ID != "t1" {
		t.Errorf("transactions: %+v", bank.Transactions)
	}

	// Run again with the depot set back, it returns to Accounts.
	bank.setRoles(map[string]AccountRole{"giro": RoleBusiness})
	if len(bank.Accounts) != 2 || len(bank.Ignored) != 0 {
		t.Errorf("set back: accounts %+v, ignored %+v", bank.Accounts, bank.Ignored)
	}

	ninja := &NinjaDataset{BankAccounts: []NinjaBankAccount{{Key: "giro"}, {Key: "depot"}}, BankTxns: []NinjaBankTxn{{Key: "b1", AccountKey: "giro"}, {Key: "b2", AccountKey: "depot"}}}
	ninja.setRoles(roles)
	if len(ninja.BankAccounts) != 2 || len(ninja.BankTxns) != 1 || ninja.BankTxns[0].Key != "b1" {
		t.Errorf("ninja: %+v %+v", ninja.BankAccounts, ninja.BankTxns)
	}
}

// Sure's kind suggests ignoring what holds no money to spend, and only
// while the user has said nothing.
func TestSuggestedRole(t *testing.T) {
	for _, c := range []struct {
		a    BankAccount
		want AccountRole
	}{
		{BankAccount{Type: "investment"}, RoleIgnored},
		{BankAccount{Type: "crypto"}, RoleIgnored},
		{BankAccount{Type: "vehicle"}, RoleIgnored},
		{BankAccount{Type: "depository"}, RoleUnknown},
		{BankAccount{Type: "investment", Role: RolePrivate}, RoleUnknown},
	} {
		if got := c.a.Suggested(); got != c.want {
			t.Errorf("%+v: %q, want %q", c.a, got, c.want)
		}
	}
}
