package metrics

import (
	"testing"

	"andon/internal/sources"
)

// A merchant's bookings without category take the category its filed
// ones agree on; a split vote is proposed but not ticked; transfers
// stay out.
func TestUncategorized(t *testing.T) {
	bank := &sources.SureDataset{Transactions: []sources.SureTxn{
		{ID: "1", Date: "2026-10-01", Merchant: "DKB AG", Amount: -2.5},
		{ID: "2", Date: "2026-10-05", Merchant: "DKB AG", Amount: -2.5},
		{ID: "3", Date: "2026-09-01", Merchant: "DKB AG", CategoryID: "fees"},
		{ID: "4", Date: "2026-10-02", Name: "Bäckerei", Amount: -6.4},
		{ID: "5", Date: "2026-09-02", Name: "Bäckerei", CategoryID: "food"},
		{ID: "6", Date: "2026-09-03", Name: "Bäckerei", CategoryID: "snacks"},
		{ID: "7", Date: "2026-10-03", Name: "Umbuchung", Transfer: true},
	}}
	got := Uncategorized(bank)
	if len(got) != 2 {
		t.Fatalf("cases: %+v", got)
	}
	dkb, bakery := got[0], got[1]
	if dkb.Key != "m-dkb-ag" || len(dkb.Txns) != 2 || dkb.Txns[0].ID != "2" || dkb.CategoryID != "fees" || !dkb.Ticked || dkb.Total != -5 {
		t.Errorf("dkb: %+v", dkb)
	}
	if bakery.CategoryID != "food" || bakery.Votes != 1 || bakery.Of != 2 || bakery.Ticked {
		t.Errorf("bakery: %+v", bakery)
	}
}
