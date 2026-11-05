package metrics

// The VAT return's figures (ELSTER Umsatzsteuer-Voranmeldung) from
// Invoice Ninja, for one period. Bases are whole euros (cut, as ELSTER
// takes them), taxes cents:
//
//	Kz 81 / 86   revenue at 19 % / 7 %, net
//	Kz 21        services to EU businesses (0 %, reverse charge there),
//	             by invoice date for either method (§ 18b UStG), as the ZM
//	Kz 45        other non-taxable revenue (0 %, client outside the EU)
//	Kz 46 / 47   EU services received under § 13b: base / tax
//	Kz 84 / 85   other § 13b services (outside the EU): base / tax
//	Kz 66        input VAT from invoices
//	Kz 67        input VAT from § 13b (47 + 85)
//	Kz 83        due: output VAT − input VAT
//
// Reverse charge: an expense without tax from a vendor abroad. Andon
// cannot tell goods from services; the page says so.
//
// What cannot be placed is not guessed: it stays out of the figures, or
// in as Invoice Ninja has it, and Checks names it for the user.

import (
	"math"
	"slices"
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
// numeric country ids (ISO 3166-1), with their VAT ID prefix (Greece EL).
var euCountries = map[string]string{"40": "AT", "56": "BE", "100": "BG", "191": "HR", "196": "CY", "203": "CZ", "208": "DK",
	"233": "EE", "246": "FI", "250": "FR", "300": "EL", "348": "HU", "372": "IE", "380": "IT", "428": "LV", "440": "LT",
	"442": "LU", "470": "MT", "528": "NL", "616": "PL", "620": "PT", "642": "RO", "703": "SK", "705": "SI", "724": "ES", "752": "SE"}

// InEU reports whether an Invoice Ninja country id is an EU member
// besides Germany.
func InEU(countryID string) bool { return euCountries[countryID] != "" }

// Elster holds the period's figures.
type Elster struct {
	Kz81, Kz86, Kz21, Kz45, Kz46, Kz84 float64 // bases, whole euros
	Kz47, Kz85, Kz66, Kz67, Kz83       float64 // taxes
	Reverse                            []ReverseCharge
	Checks                             []ElsterCheck
}

// CheckKind is why a booking needs a look before the return is filed.
type CheckKind string

const (
	CheckRate       CheckKind = "rate"       // an invoice's rate fits no line
	CheckZero       CheckKind = "zero"       // 0 %, the client at home or of unknown country
	CheckForeignTax CheckKind = "foreign"    // foreign VAT: not input VAT here, not in Kz 66
	CheckNoCountry  CheckKind = "no_country" // no tax, vendor of unknown country: § 13b not tested
	CheckTaxOnTop   CheckKind = "tax_on_top" // the bank's gross amount booked as net
	CheckRefund     CheckKind = "refund"     // a payment refunded: not taken off, reduce by hand
)

// ElsterCheck is one booking the figures may be wrong about.
type ElsterCheck struct {
	Kind   CheckKind
	Day    string
	Name   string  // client or vendor
	Number string  // invoice or expense number
	Amount float64 // net invoiced, the expense's amount, or gross refunded
	Diff   float64 // CheckTaxOnTop: input VAT counted too much
}

// CheckGroup is the checks of one kind, for the page.
type CheckGroup struct {
	Kind  CheckKind
	Items []ElsterCheck
	Diff  float64 // the items' sum
}

// CheckGroups lists the checks by kind, in the order they were found.
func (e Elster) CheckGroups() []CheckGroup {
	var out []CheckGroup
	for _, c := range e.Checks {
		i := slices.IndexFunc(out, func(g CheckGroup) bool { return g.Kind == c.Kind })
		if i < 0 {
			out = append(out, CheckGroup{Kind: c.Kind})
			i = len(out) - 1
		}
		out[i].Items = append(out[i].Items, c)
		out[i].Diff = round2(out[i].Diff + c.Diff)
	}
	return out
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
	// Services to EU businesses count by invoice date either way (below).
	var b81, b86, b21, b45 float64
	seen := map[invoiceRef]int{}
	check := func(kind CheckKind, inv sources.NinjaInvoice, net float64) {
		ref := invoiceRef{inv.ID, inv.Number}
		if i, ok := seen[ref]; ok {
			e.Checks[i].Amount = round2(e.Checks[i].Amount + net)
			return
		}
		seen[ref] = len(e.Checks)
		e.Checks = append(e.Checks, ElsterCheck{Kind: kind, Day: inv.Date, Name: clients[inv.ClientID].Name, Number: inv.Number, Amount: round2(net)})
	}
	add := func(inv sources.NinjaInvoice, share float64) {
		net := inv.Net * share
		if net == 0 {
			return
		}
		country := clients[inv.ClientID].CountryID
		switch rate := invoiceRate(inv); {
		case math.Abs(rate-rateFull) <= rateTol:
			b81 += net
		case math.Abs(rate-rateReduced) <= rateTol:
			b86 += net
		case rate == 0 && InEU(country):
			// by invoice date, below
		case rate == 0 && (country == "" || country == n.HomeCountryID):
			check(CheckZero, inv, net)
		case rate == 0:
			b45 += net
		default:
			check(CheckRate, inv, net)
		}
	}
	counted := NinjaCounted(n)
	for _, inv := range counted {
		if in(inv.Date) && invoiceRate(inv) == 0 && InEU(clients[inv.ClientID].CountryID) {
			b21 += inv.Net
		}
	}
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

	// A refund reduces the month it happened: the payment's month stays
	// as filed, and the page asks in the month of the payment's last
	// change, the nearest Invoice Ninja says.
	for _, p := range n.Payments {
		if p.Refunded != 0 && in(p.RefundDay) {
			e.Checks = append(e.Checks, ElsterCheck{Kind: CheckRefund, Day: p.RefundDay, Name: clients[p.ClientID].Name, Number: p.Number, Amount: p.Refunded})
		}
	}

	// Input VAT and reverse charge from the period's expenses.
	vendors := vendorsByKey(n)
	txns := map[string]sources.NinjaBankTxn{}
	for _, t := range n.BankTxns {
		txns[t.Key] = t
	}
	home := n.HomeCountryID
	var b46, b84 float64
	for _, x := range n.Expenses {
		if !in(x.Date) {
			continue
		}
		v := vendors[x.VendorKey]
		note := ElsterCheck{Day: x.Date, Name: v.Name, Number: x.Number, Amount: x.Amount}
		if foreignVAT(x, v, home) {
			note.Kind = CheckForeignTax
			e.Checks = append(e.Checks, note)
			continue
		}
		e.Kz66 += x.Tax
		if over := taxOnTop(x, txns[x.TxnKey]); over > 0 {
			note.Kind, note.Diff = CheckTaxOnTop, over
			e.Checks = append(e.Checks, note)
		}
		if x.Tax != 0 || v.CountryID == home {
			continue
		}
		if x.VendorKey == "" {
			continue // no vendor (a bank fee): nothing to look up
		}
		if v.CountryID == "" {
			note.Kind = CheckNoCountry
			e.Checks = append(e.Checks, note)
			continue
		}
		rc := ReverseCharge{Expense: x, Vendor: v.Name, EU: InEU(v.CountryID)}
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

// invoiceRef tells invoices apart, whichever of the two the data has.
type invoiceRef struct {
	id     int64
	number string
}

// vendorsByKey indexes a dataset's vendors.
func vendorsByKey(n *sources.NinjaDataset) map[string]sources.NinjaVendor {
	out := make(map[string]sources.NinjaVendor, len(n.Vendors))
	for _, v := range n.Vendors {
		out[v.Key] = v
	}
	return out
}

// foreignVAT says whether an expense carries another country's VAT: a
// vendor abroad at a rate Germany has not, or at none named. German VAT
// from abroad (a vendor registered here) is input VAT; a country with
// the same rate cannot be told from it.
func foreignVAT(x sources.NinjaExpense, v sources.NinjaVendor, home string) bool {
	if x.Tax == 0 || v.CountryID == "" || v.CountryID == home {
		return false
	}
	return math.Abs(x.TaxRate-rateFull) > rateTol && math.Abs(x.TaxRate-rateReduced) > rateTol
}

// taxOnTop is the input VAT an expense counts too much: its booking's
// whole amount taken as net, tax on top of what was paid. 0 if not.
func taxOnTop(x sources.NinjaExpense, t sources.NinjaBankTxn) float64 {
	if x.TaxRate <= 0 || math.Abs(t.Amount-x.Amount) > amountTol {
		return 0
	}
	if over := round2(x.Tax - x.Amount*x.TaxRate/(100+x.TaxRate)); over >= amountTol {
		return over
	}
	return 0
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
