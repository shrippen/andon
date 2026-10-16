package metrics

// The VAT return's figures (ELSTER Umsatzsteuer-Voranmeldung) from
// Invoice Ninja, for one period. Bases are whole euros (cut, as ELSTER
// takes them), taxes cents:
//
//	Kz 81 / 86   revenue at 19 % / 7 %, net
//	Kz 21        services to EU businesses (0 %, reverse charge there)
//	Kz 45        other non-taxable revenue (0 %, outside the EU)
//	Kz 46 / 47   EU services received under § 13b: base / tax
//	Kz 84 / 85   other § 13b services (outside the EU): base / tax
//	Kz 66        input VAT from invoices
//	Kz 67        input VAT from § 13b (47 + 85)
//	Kz 83        due: output VAT − input VAT
//
// Reverse charge: an expense without tax from a vendor abroad. Andon
// cannot tell goods from services; the page says so.

import (
	"math"
	"time"

	"andon/internal/sources"
)

// The rates Germany taxes at, and the reverse charge rate.
const (
	rateFull    = 19.0
	rateReduced = 7.0
	rateTol     = 0.6 // percent points an invoice's rate may stray (rounding)
)

// euCountries are the EU members besides Germany, as Invoice Ninja's
// numeric country ids (ISO 3166-1).
var euCountries = map[string]bool{"40": true, "56": true, "100": true, "191": true, "196": true, "203": true, "208": true,
	"233": true, "246": true, "250": true, "300": true, "348": true, "372": true, "380": true, "428": true, "440": true,
	"442": true, "470": true, "528": true, "616": true, "620": true, "642": true, "703": true, "705": true, "724": true, "752": true}

// InEU reports whether an Invoice Ninja country id is an EU member
// besides Germany.
func InEU(countryID string) bool { return euCountries[countryID] }

// Elster holds the period's figures.
type Elster struct {
	Kz81, Kz86, Kz21, Kz45, Kz46, Kz84 float64 // bases, whole euros
	Kz47, Kz85, Kz66, Kz67, Kz83       float64 // taxes
	Reverse                            []ReverseCharge
}

// ReverseCharge is an expense from abroad without tax.
type ReverseCharge struct {
	Expense sources.NinjaExpense
	Vendor  string
	EU      bool // Kz 46/47, else 84/85
}

// Figure is one line of the return, for the page.
type Figure struct {
	Kz    string
	Value float64
	Base  bool // a base in whole euros, else a tax in cents
}

// Figures lists the non-empty lines in the return's order; Kz 83 always.
func (e Elster) Figures() []Figure {
	all := []Figure{{"21", e.Kz21, true}, {"45", e.Kz45, true}, {"81", e.Kz81, true}, {"86", e.Kz86, true},
		{"46", e.Kz46, true}, {"47", e.Kz47, false}, {"84", e.Kz84, true}, {"85", e.Kz85, false},
		{"66", e.Kz66, false}, {"67", e.Kz67, false}}
	var out []Figure
	for _, f := range all {
		if f.Value != 0 {
			out = append(out, f)
		}
	}
	return append(out, Figure{"83", e.Kz83, false})
}

// ElsterOf computes the figures of [start, end] by the method ("ist":
// payments, "soll": invoice dates).
func ElsterOf(n *sources.NinjaDataset, start, end time.Time, method string) Elster {
	var e Elster
	if n == nil {
		return e
	}
	clients := map[int64]sources.NinjaClient{}
	for _, c := range n.Clients {
		clients[c.ID] = c
	}
	in := func(day string) bool {
		d, ok := ParseDay(day)
		return ok && !d.Before(start) && !d.After(end)
	}

	// Revenue: net per rate, from invoices (soll) or their payments (ist).
	var b81, b86, b21, b45 float64
	add := func(inv sources.NinjaInvoice, share float64) {
		net := inv.Net * share
		switch rate := invoiceRate(inv); {
		case math.Abs(rate-rateFull) <= rateTol:
			b81 += net
		case math.Abs(rate-rateReduced) <= rateTol:
			b86 += net
		case rate == 0 && euCountries[clients[inv.ClientID].CountryID]:
			b21 += net
		case rate == 0:
			b45 += net
		}
	}
	counted := NinjaCounted(n)
	if method == "soll" {
		for _, inv := range counted {
			if in(inv.Date) {
				add(inv, 1)
			}
		}
	} else {
		byClient := map[int64][]sources.NinjaInvoice{}
		for _, inv := range counted {
			byClient[inv.ClientID] = append(byClient[inv.ClientID], inv)
		}
		for _, p := range n.Payments {
			if !in(p.Date) {
				continue
			}
			// A payment pays its client's invoices in proportion, as the
			// output VAT does (NinjaOutputVAT).
			var gross float64
			for _, inv := range byClient[p.ClientID] {
				gross += inv.Amount
			}
			for _, inv := range byClient[p.ClientID] {
				if gross > 0 {
					add(inv, p.Amount/gross)
				}
			}
		}
	}

	// Input VAT and reverse charge from the period's expenses.
	vendors := map[string]sources.NinjaVendor{}
	for _, v := range n.Vendors {
		vendors[v.Key] = v
	}
	home := n.HomeCountryID
	var b46, b84 float64
	for _, x := range n.Expenses {
		if !in(x.Date) {
			continue
		}
		e.Kz66 += x.Tax
		v := vendors[x.VendorKey]
		if x.Tax != 0 || v.CountryID == "" || v.CountryID == home {
			continue
		}
		rc := ReverseCharge{Expense: x, Vendor: v.Name, EU: euCountries[v.CountryID]}
		e.Reverse = append(e.Reverse, rc)
		if rc.EU {
			b46 += x.Amount
		} else {
			b84 += x.Amount
		}
	}

	e.Kz81, e.Kz86, e.Kz21, e.Kz45, e.Kz46, e.Kz84 = math.Floor(b81), math.Floor(b86), math.Floor(b21), math.Floor(b45), math.Floor(b46), math.Floor(b84)
	e.Kz47, e.Kz85 = round2(e.Kz46*rateFull/100), round2(e.Kz84*rateFull/100)
	e.Kz66, e.Kz67 = round2(e.Kz66), round2(e.Kz47+e.Kz85)
	output := e.Kz81*rateFull/100 + e.Kz86*rateReduced/100 + e.Kz47 + e.Kz85
	e.Kz83 = round2(output - e.Kz66 - e.Kz67)
	return e
}

// invoiceRate is an invoice's tax rate in percent, from its tax and net.
func invoiceRate(inv sources.NinjaInvoice) float64 {
	if inv.Net == 0 {
		return 0
	}
	return inv.Taxes / inv.Net * 100
}

// Changed lists the figures that differ from those filed (kz → value).
func (e Elster) Changed(filed map[string]float64) []Figure {
	if len(filed) == 0 {
		return nil
	}
	var out []Figure
	seen := map[string]bool{}
	for _, f := range e.Figures() {
		seen[f.Kz] = true
		if math.Abs(filed[f.Kz]-f.Value) >= 1 {
			out = append(out, f)
		}
	}
	for kz, v := range filed {
		if !seen[kz] && math.Abs(v) >= 1 {
			out = append(out, Figure{Kz: kz})
		}
	}
	return out
}
