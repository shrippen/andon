package metrics

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// A reserve account is no free cash, and it covers taxes before the
// free cash does.
func TestSafeToSpendReserve(t *testing.T) {
	sure := &sources.SureDataset{Accounts: []sources.SureAccount{
		{Name: "Geschäft", Type: "depository", Balance: 8000, Role: sources.RoleBusiness},
		{Name: "Rücklage", Type: "depository", Balance: 3000, Role: sources.RoleReserve},
	}}
	s := SafeToSpend(sure, &sources.NinjaDataset{}, time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), "", "", 0.3)
	if s.Cash != 8000 || s.Reserve != 3000 || s.Free != 8000 {
		t.Errorf("spendable: %+v", s)
	}

	s = Spendable{Cash: 8000, Reserve: 3000, VAT: 1500, IncomeTax: 2500, Fixed: 900}
	if got := s.Taxes(); got != 1000 {
		t.Errorf("uncovered taxes = %v, want 1000", got)
	}
	if s.Reserve = 5000; s.Taxes() != 0 {
		t.Errorf("covered taxes = %v, want 0", s.Taxes())
	}
}
