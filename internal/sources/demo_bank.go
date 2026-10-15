package sources

// The demo's Invoice Ninja bank feed, built from its expenses and the
// demo Sure, so the VAT check finds one case of each kind:
//
//	older expenses          booked, linked to their transaction
//	the two newest          an expense exists, the transaction is open
//	Kabelwerk               no expense; history tells vendor, category, tax
//	Nordhost                no expense, no history; Sure knows the merchant
//	the overdue invoice     paid, the income is open
//	the last payment        booked by hand in Ninja, its income is open
//	a scan without expense  its debit is open: the new expense gets the scan
//	office supplies         unknown payee: a case for the language model
//	music streaming         private, paid from the business account
//	private account         train pass (business, paid privately) and
//	                        groceries, set apart; a refund drops out

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
	demoHostNext   = 25 // days to the server's next booking

	// The feed's two accounts; the demo connection names their roles.
	demoBusiness    = "Geschäftskonto"
	demoPrivate     = "Privatkonto"
	demoBusinessKey = "demo-a1"
	demoPrivateKey  = "demo-a2"
)

func demoNinjaBank(today time.Time, data *NinjaDataset) {
	data.BankRead = true
	data.BankAccounts = []NinjaBankAccount{{Key: demoBusinessKey, Name: demoBusiness}, {Key: demoPrivateKey, Name: demoPrivate}}
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

	demoEuer(data)

	// The server's recurring expense still has last year's price; Sure
	// already books the new one.
	data.RecurringExpenses = []NinjaRecurringExpense{{Key: "demo-r1", Number: "WA-001", VendorKey: vendorKey(host.Name),
		CategoryKey: categoryKey(categoryOf[host.Name]), Amount: host.Monthly, Months: 1, Active: true,
		Next: iso(today.AddDate(0, 0, demoHostNext)), Notes: host.Contract.DE()}}

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
			Amount: music.Monthly, Debit: true, Status: BankOpen},
		NinjaBankTxn{Key: "demo-p-ticket", Date: iso(today.AddDate(0, 0, -12)), Description: "Deutschlandticket " + today.Format("01/2006"),
			Payer: "DB Fernverkehr AG", Amount: 58, Debit: true, Status: BankOpen, Account: demoPrivate, AccountKey: demoPrivateKey},
		NinjaBankTxn{Key: "demo-p-food", Date: iso(today.AddDate(0, 0, -9)), Description: "Einkauf", Payer: "Wochenmarkt Eimsbüttel",
			Amount: 38.4, Debit: true, Status: BankOpen, Account: demoPrivate, AccountKey: demoPrivateKey},
		NinjaBankTxn{Key: "demo-p-refund", Date: iso(today.AddDate(0, 0, -13)), Description: "Erstattung", Payer: "Krankenkasse",
			Amount: 120, Status: BankOpen, Account: demoPrivate, AccountKey: demoPrivateKey})

	// A scan in Paperless waits for its expense; its debit is open.
	for _, r := range demoReceipts(today) {
		if r.expense == "" && len(r.docs) > 0 && iso(r.day) >= since {
			data.BankTxns = append(data.BankTxns, NinjaBankTxn{Key: "demo-b-scan", Date: iso(r.day), Description: r.notes,
				Payer: r.vendor, Amount: r.amount, Debit: true, Status: BankOpen})
			break
		}
	}

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
	for i := range data.Payments {
		p := &data.Payments[i]
		p.Key, p.Number = fmt.Sprintf("demo-pay%d", p.ID), fmt.Sprintf("Z-%04d", p.ID)
	}
	if n := len(data.Payments); n > 0 && data.Payments[n-1].Date >= since {
		last := data.Payments[n-1]
		data.BankTxns = append(data.BankTxns, NinjaBankTxn{Key: "demo-b-booked", Date: last.Date, Description: "Zahlung",
			Payer: demoClientName(data, last.ClientID), Amount: last.Amount, Status: BankOpen})
	}
	for i := range data.BankTxns {
		if data.BankTxns[i].Account == "" {
			data.BankTxns[i].Account, data.BankTxns[i].AccountKey = demoBusiness, demoBusinessKey
		}
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

// demoEuerSlot holds the EÜR line in the demo's expenses (custom field 3).
const demoEuerSlot = 2

// demoEuerLines are the demo's EÜR dropdown, lines of the Anlage EÜR.
var demoEuerLines = []string{"Laufende EDV-Kosten", "Aufwendungen für Telekommunikation", "Arbeitsmittel",
	"Übernachtungs- und Reisenebenkosten", "Werbekosten", "Übrige unbeschränkt abziehbare Betriebsausgaben"}

// demoEuer gives the demo an EÜR field; two in three expenses carry their
// line, the rest wait for the EÜR workbench.
func demoEuer(data *NinjaDataset) {
	data.ExpenseFields[demoEuerSlot] = ExpenseField{Label: "EÜR", Options: demoEuerLines}
	names := map[string]string{}
	for _, c := range data.Categories {
		names[c.Key] = strings.ToLower(c.Name)
	}
	for i := range data.Expenses {
		if i%3 == 0 {
			continue
		}
		e := &data.Expenses[i]
		e.Custom[demoEuerSlot] = demoEuerLine(names[e.CategoryKey])
	}
}

// demoEuerLine is a category's EÜR line by a word in its name.
func demoEuerLine(category string) string {
	for word, line := range map[string]int{"host": 0, "software": 0, "cloud": 0, "telefon": 1, "mobil": 1, "internet": 1,
		"büro": 2, "technik": 2, "studio": 2, "reise": 3, "bahn": 3, "hotel": 3, "werbung": 4, "marketing": 4} {
		if strings.Contains(category, word) {
			return demoEuerLines[line]
		}
	}
	return demoEuerLines[len(demoEuerLines)-1]
}
