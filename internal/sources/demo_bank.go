package sources

// The demo's Invoice Ninja bank feed, built from its expenses and the
// demo Sure, so the VAT check finds one case of each kind:
//
//	older expenses          booked, linked to their transaction
//	the two newest          an expense exists, the transaction is open
//	Kabelwerk               no expense; history tells vendor, category, tax
//	Nordhost                no expense, no history; Sure knows the merchant
//	the overdue invoice     paid, the income is open
//	office supplies         unknown payee: a case for the language model
//	music streaming         private, paid from the business account

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Demo tax rates and the rate the expenses carry.
const (
	demoTaxName    = "MwSt."
	demoTaxRate    = 19
	demoTaxReduced = 7
	demoOpenLinks  = 2  // newest expenses whose transaction is still open
	demoBankDays   = 95 // how far back the feed reaches
)

func demoNinjaBank(today time.Time, data *NinjaDataset) {
	data.BankRead = true
	data.TaxRates = []NinjaTaxRate{{Name: demoTaxName, Rate: demoTaxRate}, {Name: demoTaxName + " 7 %", Rate: demoTaxReduced}}

	vendors, categories := map[string]string{}, map[string]string{}
	vendorKey := func(name string) string {
		if k, ok := vendors[name]; ok {
			return k
		}
		k := fmt.Sprintf("demo-v%d", len(vendors)+1)
		vendors[name] = k
		data.Vendors = append(data.Vendors, NinjaVendor{Key: k, Name: name})
		return k
	}
	categoryKey := func(name string) string {
		if k, ok := categories[name]; ok {
			return k
		}
		k := fmt.Sprintf("demo-c%d", len(categories)+1)
		categories[name] = k
		data.Categories = append(data.Categories, NinjaCategory{Key: k, Name: name})
		return k
	}
	categoryOf := map[string]string{}
	for _, r := range demoWorld.Receipts {
		categoryOf[r.Vendor] = r.Category.DE()
	}
	for _, v := range demoWorld.Vendors {
		categoryOf[v.Name] = v.Kind.DE()
	}

	host := demoWorld.Vendors[demoVendor("nordhost")]
	since := iso(today.AddDate(0, 0, -demoBankDays))
	sort.SliceStable(data.Expenses, func(i, j int) bool { return data.Expenses[i].Date > data.Expenses[j].Date })
	for i := range data.Expenses {
		e := &data.Expenses[i]
		name, _, _ := strings.Cut(e.Notes, " · ")
		e.Key = fmt.Sprintf("demo-e%d", e.ID)
		e.VendorKey, e.CategoryKey = vendorKey(name), categoryKey(categoryOf[name])
		e.TaxName, e.TaxRate = demoTaxName, demoTaxRate
		if e.Date < since {
			continue
		}
		txn := NinjaBankTxn{Key: "demo-b" + e.Key, Date: e.Date, Description: name + " " + e.Number, Payer: name,
			Amount: e.Amount, Debit: true, Status: BankOpen}
		if i >= demoOpenLinks {
			txn.Status, txn.ExpenseKeys, txn.VendorKey, txn.CategoryKey = BankDone, []string{e.Key}, e.VendorKey, e.CategoryKey
			e.TxnKey = txn.Key
		}
		data.BankTxns = append(data.BankTxns, txn)
	}

	// Open debits without an expense; the Sure ones carry Sure's names.
	studio := demoWorld.Receipts[demoReceiptOf(demoStudioReceipt)]
	music := demoWorld.Vendors[demoVendor("wellenklang")]
	data.BankTxns = append(data.BankTxns,
		NinjaBankTxn{Key: "demo-b-host", Date: iso(today.AddDate(0, 0, -5)), Description: host.Name + " Rechnung " + today.Format("2006-01"),
			Payer: host.Name, Amount: host.Monthly, Debit: true, Status: BankOpen},
		NinjaBankTxn{Key: "demo-b-studio", Date: iso(today.AddDate(0, 0, -8)), Description: "KW-56107 Lichtstative", Payer: studio.Vendor,
			Amount: 899, Debit: true, Status: BankOpen},
		NinjaBankTxn{Key: "demo-b-office", Date: iso(today.AddDate(0, 0, -11)), Description: "Bestellung 302-4471 Toner, Papier", Payer: "BüroDirekt24",
			Amount: 47.6, Debit: true, Status: BankOpen},
		NinjaBankTxn{Key: "demo-b-music", Date: iso(today.AddDate(0, 0, -16)), Description: music.Contract.DE(), Payer: music.Name,
			Amount: music.Monthly, Debit: true, Status: BankOpen})

	// The overdue invoice, paid: open until it is booked.
	for _, inv := range data.Invoices {
		if inv.Balance > 0 && inv.Status == "sent" {
			data.BankTxns = append(data.BankTxns, NinjaBankTxn{Key: "demo-b-paid", Date: iso(today.AddDate(0, 0, -3)),
				Description: inv.Number, Payer: demoClientName(data, inv.ClientID), Amount: inv.Balance, Status: BankOpen})
			break
		}
	}
	for i := range data.Invoices {
		data.Invoices[i].Key = fmt.Sprintf("demo-i%d", data.Invoices[i].ID)
	}
}

// demoVendor is the index of a world vendor by id; it must exist.
func demoVendor(id string) int {
	for i, v := range demoWorld.Vendors {
		if v.ID == id {
			return i
		}
	}
	panic("demo vendor missing: " + id)
}

// demoStudioReceipt is the world's receipt from the studio supplier,
// whose next bill has no expense yet. Names stay in world.json: release
// builds must not carry them (scripts/release-check.sh).
const demoStudioReceipt = 2

// demoReceiptOf is the index of a world receipt by id; it must exist.
func demoReceiptOf(id int) int {
	for i, r := range demoWorld.Receipts {
		if r.ID == id {
			return i
		}
	}
	panic(fmt.Sprintf("demo receipt missing: %d", id))
}

// demoClientName is the name of a client of the dataset.
func demoClientName(data *NinjaDataset, id int64) string {
	for _, c := range data.Clients {
		if c.ID == id {
			return c.Name
		}
	}
	return ""
}
