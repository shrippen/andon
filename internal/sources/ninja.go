package sources

import (
	"context"
	"hash/fnv"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
)

var ninjaStatus = map[string]string{
	"1": "draft", "2": "sent", "3": "partial", "4": "paid", "5": "cancelled", "6": "reversed",
}

var quoteStatus = map[string]string{
	"1": "draft", "2": "sent", "3": "approved", "4": "converted", "-1": "expired",
}

func ninjaAPI(sctx Ctx) (services.NinjaApi, error) {
	secret, err := needSecret(sctx)
	if err != nil {
		return services.NinjaApi{}, err
	}
	return services.NinjaApi{URL: sctx.URL, Token: secret, Verify: sctx.VerifyTLS}, nil
}

func ninjaInvoice(raw any) NinjaInvoice {
	m := asMap(raw)
	amount, taxes := asFloat(m["amount"]), asFloat(m["total_taxes"])
	// Reminders sent so far; a last-sent date counts as one at least.
	stage := 0
	for _, k := range []string{"reminder1_sent", "reminder2_sent", "reminder3_sent"} {
		if day(m[k]) != "" {
			stage++
		}
	}
	if stage == 0 && day(m["reminder_last_sent"]) != "" {
		stage = 1
	}
	return NinjaInvoice{
		Stage: stage,
		ID:    refNum(m["id"]), Key: idKey(m["id"]), Number: asStr(m["number"]), ClientID: refNum(m["client_id"]),
		Status: statusFor(ninjaStatus, m["status_id"]), Date: day(m["date"]), DueDate: day(m["due_date"]),
		Amount: amount, Balance: asFloat(m["balance"]), Taxes: taxes, Net: amount - taxes,
		Reminded: day(m["reminder_last_sent"]), NextSend: day(m["next_send_date"]),
		Items: ninjaItems(m["line_items"]),
	}
}

// Activities of an invoice's history that are reminders, by stage.
var reminderActivity = map[int]int{63: 1, 64: 2, 65: 3}

// historyLookups caps the history calls of one fetch, historyParallel
// how many run at once.
const (
	historyLookups  = 40
	historyParallel = 6
)

// remindersFromHistory reads the stage of overdue invoices from their
// history: a reminder sent by hand (Andon's dunning run, Ninja's own
// button) leaves reminder1_sent empty, only an activity names it.
func remindersFromHistory(ctx context.Context, api services.NinjaApi, invoices []NinjaInvoice, today string) {
	var overdue []*NinjaInvoice
	for i := range invoices {
		inv := &invoices[i]
		if inv.Balance > 0 && inv.DueDate != "" && inv.DueDate < today && inv.Key != "" {
			overdue = append(overdue, inv)
		}
	}
	// The most recently due first: they are the ones a reminder is
	// sent for now; past the cap, older ones keep the fields' stage.
	slices.SortFunc(overdue, func(a, b *NinjaInvoice) int { return strings.Compare(b.DueDate, a.DueDate) })
	if len(overdue) > historyLookups {
		overdue = overdue[:historyLookups]
	}

	var wg sync.WaitGroup
	slots := make(chan struct{}, historyParallel)
	for _, inv := range overdue {
		wg.Add(1)
		slots <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-slots }()
			raw, err := api.Post(ctx, "activities/entity", map[string]string{"entity": "invoice", "entity_id": inv.Key})
			if err != nil {
				return
			}
			for _, a := range asList(asMap(raw)["data"]) {
				am := asMap(a)
				stage, ok := reminderActivity[int(asFloat(am["activity_type_id"]))]
				if !ok {
					continue
				}
				inv.Stage = max(inv.Stage, stage)
				if at := time.Unix(int64(asFloat(am["created_at"])), 0).UTC().Format(time.DateOnly); at > inv.Reminded {
					inv.Reminded = at
				}
			}
		}()
	}
	wg.Wait()
}

// itemChars caps a line item's text: enough to find "Fahrtkosten".
const itemChars = 80

func ninjaItems(raw any) []string {
	var out []string
	for _, l := range asList(raw) {
		lm := asMap(l)
		text := strings.ToLower(strings.TrimSpace(asStr(lm["product_key"]) + " " + asStr(lm["notes"])))
		if r := []rune(text); len(r) > itemChars {
			text = string(r[:itemChars])
		}
		if text != "" {
			out = append(out, text)
		}
	}
	return out
}

func statusFor(table map[string]string, raw any) string {
	key := asStr(raw)
	if key == "" {
		key = itoa(asInt64(raw))
	}
	if s, ok := table[key]; ok {
		return s
	}
	return "draft"
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// expenseTax is an expense's input VAT: explicit tax amounts, else computed
// from the rate (inclusive or exclusive).
func expenseTax(m map[string]any) float64 {
	amounts := asFloat(m["tax_amount1"]) + asFloat(m["tax_amount2"]) + asFloat(m["tax_amount3"])
	if amounts != 0 {
		return amounts
	}
	rate := asFloat(m["tax_rate1"]) + asFloat(m["tax_rate2"]) + asFloat(m["tax_rate3"])
	amount := asFloat(m["amount"])
	if rate == 0 {
		return 0
	}
	if asBool(m["uses_inclusive_taxes"]) {
		return amount * rate / (100 + rate)
	}
	return amount * rate / 100
}

// NinjaData is the "invoiceninja.data" source: invoices, payments, clients,
// expenses, quotes and recurring invoices from the last window.
var NinjaData = source{key: "invoiceninja.data", ttl: dataTTL, service: enums.ServiceInvoiceNinja, fetch: fetchNinja}

func fetchNinja(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		data := DemoNinja(time.Now())
		data.setRoles(accountRoles(sctx.Options))
		return data, nil
	}
	api, err := ninjaAPI(sctx)
	if err != nil {
		return nil, err
	}
	data, err := loadNinja(ctx, api, sctx)
	if err != nil {
		return nil, fetchError(err)
	}
	data.setRoles(accountRoles(sctx.Options))
	return data, nil
}

func loadNinja(ctx context.Context, api services.NinjaApi, sctx Ctx) (*NinjaDataset, error) {
	since := windowStart(time.Now().UTC()).Format("2006-01-02")

	invoicesRaw, err := api.Pages(ctx, "invoices", url.Values{"is_deleted": {"false"}})
	if err != nil {
		return nil, err
	}
	var invoices []NinjaInvoice
	for _, i := range invoicesRaw {
		im := asMap(i)
		if asBool(im["is_deleted"]) {
			continue
		}
		if day(im["date"]) >= since || asFloat(im["balance"]) > 0 {
			invoices = append(invoices, ninjaInvoice(i))
		}
	}

	remindersFromHistory(ctx, api, invoices, time.Now().UTC().Format(time.DateOnly))

	paymentsRaw, err := api.Pages(ctx, "payments", url.Values{"include": {"paymentables"}, "is_deleted": {"false"}})
	if err != nil {
		return nil, err
	}
	var payments []NinjaPayment
	for _, p := range paymentsRaw {
		pm := asMap(p)
		if asBool(pm["is_deleted"]) {
			continue
		}
		if day(pm["date"]) >= since {
			payments = append(payments, NinjaPayment{
				ID: refNum(pm["id"]), Date: day(pm["date"]), Amount: asFloat(pm["amount"]),
				ClientID: refNum(pm["client_id"]), Key: idKey(pm["id"]), Number: asStr(pm["number"]),
			})
		}
	}

	clientsRaw, err := api.Pages(ctx, "clients", url.Values{"is_deleted": {"false"}})
	if err != nil {
		return nil, err
	}
	var clients []NinjaClient
	for _, c := range clientsRaw {
		cm := asMap(c)
		if asBool(cm["is_deleted"]) {
			continue
		}
		name := asStr(cm["display_name"])
		if name == "" {
			name = asStr(cm["name"])
		}
		clients = append(clients, NinjaClient{
			ID: refNum(cm["id"]), Key: idKey(cm["id"]), Name: name, VATNumber: asStr(cm["vat_number"]),
			CountryID: itoa(asInt64(cm["country_id"])),
		})
	}

	// Deleted expenses as the receipts page skips them (fetchNinjaExpenses).
	expensesRaw, err := api.Pages(ctx, "expenses", url.Values{"is_deleted": {"false"}})
	if err != nil {
		return nil, err
	}
	var expenses []NinjaExpense
	for _, e := range expensesRaw {
		em := asMap(e)
		if asBool(em["is_deleted"]) {
			continue
		}
		if day(em["date"]) >= since {
			notes := asStr(em["public_notes"])
			expenses = append(expenses, NinjaExpense{
				ID: refNum(em["id"]), Number: asStr(em["number"]), Date: day(em["date"]), Amount: asFloat(em["amount"]),
				Tax: round2(expenseTax(em)), Notes: notes, VendorID: asInt64(em["vendor_id"]), VendorKey: idKey(em["vendor_id"]),
				Key: idKey(em["id"]), CategoryKey: idKey(em["category_id"]), TaxName: asStr(em["tax_name1"]), TaxRate: asFloat(em["tax_rate1"]),
				TxnKey: idKey(em["transaction_id"]), Billable: asBool(em["should_be_invoiced"]),
			})
			last := &expenses[len(expenses)-1]
			for i := range last.Custom {
				last.Custom[i] = strings.TrimSpace(asStr(em["custom_value"+strconv.Itoa(i+1)]))
			}
		}
	}

	// Vendor names let other sources match senders to expenses.
	var vendors []NinjaVendor
	if vendorsRaw, err := api.Pages(ctx, "vendors", url.Values{"is_deleted": {"false"}}); err == nil {
		for _, v := range vendorsRaw {
			vm := asMap(v)
			if asBool(vm["is_deleted"]) {
				continue
			}
			vendors = append(vendors, NinjaVendor{Key: idKey(vm["id"]), Name: asStr(vm["name"]), CountryID: asStr(vm["country_id"]),
				Archived: asFloat(vm["archived_at"]) > 0})
		}
	}

	quotesRaw, err := api.Pages(ctx, "quotes", url.Values{"is_deleted": {"false"}})
	if err != nil {
		return nil, err
	}
	var quotes []NinjaQuote
	for _, q := range quotesRaw {
		qm := asMap(q)
		if asBool(qm["is_deleted"]) {
			continue
		}
		quotes = append(quotes, NinjaQuote{
			ID: refNum(qm["id"]), Key: idKey(qm["id"]), Number: asStr(qm["number"]), ClientID: refNum(qm["client_id"]),
			Status: statusFor(quoteStatus, qm["status_id"]), Date: day(qm["date"]), ValidUntil: day(qm["due_date"]), Amount: asFloat(qm["amount"]),
		})
	}

	recurringRaw, err := api.Pages(ctx, "recurring_invoices", url.Values{"is_deleted": {"false"}})
	if err != nil {
		return nil, err
	}
	var recurring []NinjaRecurring
	for _, r := range recurringRaw {
		rm := asMap(r)
		if asBool(rm["is_deleted"]) {
			continue
		}
		recurring = append(recurring, NinjaRecurring{
			ID: refNum(rm["id"]), Key: idKey(rm["id"]), Number: asStr(rm["number"]), ClientID: refNum(rm["client_id"]),
			Active: asStr(rm["status_id"]) == "2", NextSendDate: day(rm["next_send_date"]),
			RemainingCycles: int(asFloat(rm["remaining_cycles"])), Amount: asFloat(rm["amount"]),
		})
	}

	bank := loadNinjaBank(ctx, api, since)
	company := ninjaCompany(ctx, api)
	repeating := loadRecurringExpenses(ctx, api)

	currency, _ := sctx.Options["currency"].(string)
	if currency == "" {
		currency = "EUR"
	}
	homeCountry, _ := sctx.Options["home_country_id"].(string)
	if homeCountry == "" {
		homeCountry = "276"
	}

	return &NinjaDataset{
		URL: sctx.URL, Currency: currency, Invoices: invoices, Payments: payments, Clients: clients,
		Expenses: expenses, Vendors: vendors, Quotes: quotes, Recurring: recurring, HomeCountryID: homeCountry, Since: since,
		BankTxns: bank.txns, BankRead: bank.read, BankAccounts: bank.accounts, Categories: bank.categories, TaxRates: bank.rates,
		ExpenseFields: expenseFieldsOf(company), Reminders: remindersOf(company), RecurringExpenses: repeating,
	}, nil
}

// ninjaBank is what the VAT check reads besides the bookkeeping.
type ninjaBank struct {
	txns       []NinjaBankTxn
	read       bool
	accounts   []NinjaBankAccount
	categories []NinjaCategory
	rates      []NinjaTaxRate
}

// Invoice Ninja's bank transaction states (status_id).
var bankStatus = map[string]BankStatus{"1": BankOpen, "2": BankMatched, "3": BankDone}

// loadNinjaBank reads bank transactions since the window start (archived
// ones too: the VAT check lists them as set aside), the bank accounts, expense
// categories and tax rates. Each part is optional: an instance without
// the banking module still delivers the rest of the dataset.
func loadNinjaBank(ctx context.Context, api services.NinjaApi, since string) ninjaBank {
	var out ninjaBank
	names := map[string]string{}
	if raw, err := api.Pages(ctx, "bank_integrations", nil); err == nil {
		names = bankAccountNames(raw)
		for _, r := range raw {
			key := idKey(asMap(r)["id"])
			if name := names[key]; name != "" {
				out.accounts = append(out.accounts, NinjaBankAccount{Key: key, Name: name})
			}
		}
	}
	if raw, err := api.Pages(ctx, "bank_transactions", url.Values{"sort": {"date|desc"}, "status": {"active,archived"}}); err == nil {
		out.read = true
		for _, r := range raw {
			m := asMap(r)
			if t, ok := ninjaBankTxn(m); ok && t.Date >= since {
				t.AccountKey = idKey(m["bank_integration_id"])
				t.Account = names[t.AccountKey]
				out.txns = append(out.txns, t)
			}
		}
	}
	if raw, err := api.Pages(ctx, "expense_categories", nil); err == nil {
		for _, r := range raw {
			m := asMap(r)
			if !asBool(m["is_deleted"]) {
				out.categories = append(out.categories, NinjaCategory{Key: idKey(m["id"]), Name: asStr(m["name"])})
			}
		}
	}
	if raw, err := api.Pages(ctx, "tax_rates", nil); err == nil {
		for _, r := range raw {
			m := asMap(r)
			if !asBool(m["is_deleted"]) {
				out.rates = append(out.rates, NinjaTaxRate{Name: asStr(m["name"]), Rate: asFloat(m["rate"])})
			}
		}
	}
	return out
}

// setRoles marks each bank account and its transactions with the role
// the user named; transactions of ignored accounts leave the data. It
// runs once, on freshly fetched data (see BankData.setRoles).
func (d *NinjaDataset) setRoles(roles map[string]AccountRole) {
	for i := range d.BankAccounts {
		d.BankAccounts[i].Role = roles[d.BankAccounts[i].Key]
	}
	for i := range d.BankTxns {
		d.BankTxns[i].Role = roles[d.BankTxns[i].AccountKey]
	}
	d.BankTxns = slices.DeleteFunc(d.BankTxns, func(t NinjaBankTxn) bool { return t.Role == RoleIgnored })
}

// bankAccountNames names each bank account by id: its nickname, else the
// bank's name for it; two of one name get the end of their number
// ("Girokonto ··4711"), so each can be told business or private.
func bankAccountNames(raw []any) map[string]string {
	out, count := map[string]string{}, map[string]int{}
	name := func(m map[string]any) string {
		for _, k := range []string{"nickname", "bank_account_name", "provider_name"} {
			if v := strings.TrimSpace(asStr(m[k])); v != "" {
				return v
			}
		}
		return ""
	}
	for _, r := range raw {
		count[name(asMap(r))]++
	}
	for _, r := range raw {
		m := asMap(r)
		if asBool(m["is_deleted"]) {
			continue
		}
		n := name(m)
		if number := asStr(m["bank_account_number"]); count[n] > 1 && len(number) >= accountTail {
			n += " ··" + number[len(number)-accountTail:]
		}
		out[idKey(m["id"])] = n
	}
	return out
}

// accountTail is how many digits of an account number tell two apart.
const accountTail = 4

// ninjaBankTxn reads one bank transaction; deleted ones drop out.
func ninjaBankTxn(m map[string]any) (NinjaBankTxn, bool) {
	if asBool(m["is_deleted"]) {
		return NinjaBankTxn{}, false
	}
	status, ok := bankStatus[asStr(m["status_id"])]
	if !ok {
		status = BankOpen
	}
	payer := asStr(m["participant_name"])
	if payer == "" {
		payer = asStr(m["participant"])
	}
	return NinjaBankTxn{
		Key: idKey(m["id"]), Date: day(m["date"]), Description: strings.TrimSpace(asStr(m["description"])), Payer: strings.TrimSpace(payer),
		Amount: math.Abs(asFloat(m["amount"])), Debit: strings.EqualFold(asStr(m["base_type"]), "DEBIT"), Status: status,
		ExpenseKeys: keyList(asStr(m["expense_id"])), InvoiceKeys: keyList(asStr(m["invoice_ids"])), PaymentKey: asStr(m["payment_id"]),
		VendorKey: asStr(m["vendor_id"]), CategoryKey: asStr(m["ninja_category_id"]), Archived: asFloat(m["archived_at"]) > 0,
	}, true
}

// keyList splits Ninja's comma lists of hashed ids ("Kx9,Ab3").
func keyList(s string) []string {
	var out []string
	for _, k := range strings.Split(s, ",") {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}

// NinjaTest is the "invoiceninja.test" source: a lightweight connection check.
var NinjaTest = source{key: "invoiceninja.test", ttl: testTTL, service: enums.ServiceInvoiceNinja, fetch: fetchNinjaTest}

func fetchNinjaTest(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return map[string]any{"version": "demo"}, nil
	}
	api, err := ninjaAPI(sctx)
	if err != nil {
		return nil, err
	}
	v, err := api.Version(ctx)
	if err != nil {
		return nil, fetchError(err)
	}
	return map[string]any{"version": v}, nil
}

// refNum turns an id into a number for joins: numeric ids as they are,
// v5 hashed ids ("Opnel5aKBz") as a stable positive hash.
func refNum(v any) int64 {
	key := idKey(v)
	if n, err := strconv.ParseInt(key, 10, 64); err == nil {
		return n
	}
	if key == "" {
		return 0
	}
	h := fnv.New64a()
	h.Write([]byte(key))
	return int64(h.Sum64() >> 1)
}

// idKey reads an id that Invoice Ninja v5 sends as hashed string and
// older versions as number.
func idKey(v any) string {
	if s := asStr(v); s != "" {
		return s
	}
	if f := asFloat(v); f != 0 {
		return strconv.FormatInt(int64(f), 10)
	}
	return ""
}
