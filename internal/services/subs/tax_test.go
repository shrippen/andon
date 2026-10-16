package subs

import (
	"testing"

	"andon/internal/metrics"
	"andon/internal/services/workbench"
	"andon/internal/sources"
)

// A new recurring expense takes the tax rate chosen on the form; without
// one, its bookings would all lack input VAT. "none" books without tax,
// no field keeps the rate of the vendor's last expense.
func TestNinjaAddTax(t *testing.T) {
	b := book{
		cases: []metrics.SubCase{{Key: "na-x", Action: metrics.SubNinjaAdd, Amount: 29.99, TaxName: "USt", TaxRate: 7}},
		ninja: &sources.NinjaDataset{TaxRates: []sources.NinjaTaxRate{{Name: "MwSt", Rate: 19}, {Name: "USt", Rate: 7}}},
	}
	check := func(tax string, set bool) Item {
		t.Helper()
		s := &workbench.Step{Case: "na-x", Action: string(metrics.SubNinjaAdd), Fields: map[string]string{FieldAmount: "29,99"}}
		if set {
			s.Fields[FieldTax] = tax
		}
		it, p := b.Check(s)
		if p != workbench.ProblemNone {
			t.Fatalf("tax %q: problem %q", tax, p)
		}
		return it
	}
	if it := check("19", true); it.TaxName != "MwSt" || it.TaxRate != 19 {
		t.Errorf("19: %q %v", it.TaxName, it.TaxRate)
	}
	if it := check(noTax, true); it.TaxName != "" || it.TaxRate != 0 {
		t.Errorf("none: %q %v", it.TaxName, it.TaxRate)
	}
	if it := check("", false); it.TaxName != "USt" || it.TaxRate != 7 {
		t.Errorf("kept: %q %v", it.TaxName, it.TaxRate)
	}
}
