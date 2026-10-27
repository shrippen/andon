// Package vatcheck prepares the VAT return from Invoice Ninja's bank
// feed, with Sure beside it (matching: metrics/vatcheck.go):
//
//	Open    the period's open transactions with proposals, and the check
//	Review  the steps the user ticked, as they would be written
//	Apply   writes them, only after the user confirmed the review
//	Restore brings a transaction archived as private back
//	SaveRoles names what each bank account is for (verbund.SetRole)
//	Guess   asks the language model about the open ones, on request
//
//	  Invoice Ninja ──bank feed──┐
//	  Sure ──bookings, tags──────┼─► cases ─► review ─► confirm ─► Ninja
//	  Claude (on request) ───────┘
package vatcheck

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/assist"
	"andon/internal/services/connections"
	"andon/internal/services/receipts"
	"andon/internal/services/svcdata"
	"andon/internal/services/workbench"
	"andon/internal/sources"
)

var (
	// ErrNoNinja means the user reaches no Invoice Ninja.
	ErrNoNinja = errors.New("vatcheck.err_no_ninja")
	// ErrNoBank means Invoice Ninja's bank feed did not answer.
	ErrNoBank = errors.New("vatcheck.err_no_bank")
	// ErrDemo means a demo connection, which takes no writes.
	ErrDemo = errors.New("vatcheck.err_demo")
)

// receiptsTab is the receipts page's list of expenses without a scan.
const receiptsTab = "match"

// spansShown is how many periods the page offers, this one included.
const spansShown = 5

// need is the VAT check's services: Invoice Ninja, with Sure or Firefly
// beside it when the Verbund has one.
var need = workbench.Need{Required: []enums.ServiceType{enums.ServiceInvoiceNinja},
	Optional: []enums.ServiceType{enums.ServiceSure, enums.ServiceFirefly}}

// Page is the VAT check of one Invoice Ninja and period.
type Page struct {
	Scopes    []workbench.Scope // the Verbünde the check can work in
	Scope     string            // the one picked (workbench.Scope.Key)
	Choose    bool              // several, none picked yet: the page asks
	Conn      int64             // its Invoice Ninja
	Bank      string            // the Sure or Firefly beside it, "" none
	Spans     []metrics.VATSpan
	Span      metrics.VATSpan
	Cards     []SpanCard             // the periods, oldest first, with where each stands
	Cases     []Case                 // business, and private with a known vendor
	Apart     []Case                 // the rest of the private accounts
	Gaps      []Case                 // linked expenses of the period without EÜR line or scan (ActionFill)
	Docs      []Case                 // scans of the period no booking or expense fits (ActionReceipt)
	Skipped   []sources.NinjaBankTxn // archived in Invoice Ninja
	Accounts  []Account
	SetUp     bool  // an account has a role
	CanSet    bool  // the user may set roles (MANAGE on the connections)
	SpaceID   int64 // Invoice Ninja's space, for its Verbund page
	Check     metrics.VATCheck
	Elster    metrics.Elster   // the return's figures
	Filed     bool             // marked as filed (FileReturn)
	Changed   []metrics.Figure // figures that moved since
	Ledger    Ledger
	Euer      string   // the EÜR field's name, "" not chosen (Verbund → Kategorien)
	EuerLines []string // its choices (metrics.EuerChoices)
	Number    bool     // the invoice number has a field (receipts)
	NumberOf  string   // its name, "" unnamed
	Interval  string   // monthly, quarterly
	Method    string   // ist, soll
	Currency  string
	URL       string // Invoice Ninja, for links
	Receipts  int    // the period's expenses without a scan (receipts), -1 unknown
	Assist    bool   // the language model is set up
	Guessed   bool   // it answered for this period
}

// SpanState is where a period's return stands.
type SpanState string

const (
	SpanRunning SpanState = "running" // the period has not ended
	SpanOpen    SpanState = "open"    // ended, its deadline ahead, not filed
	SpanUnfiled SpanState = "unfiled" // its deadline passed, not marked as filed
	SpanFiled   SpanState = "filed"   // marked as filed (FileReturn)
	SpanChanged SpanState = "changed" // filed, its figures moved since
)

// SpanCard is one period of the bar on top of the page: where it stands,
// when its return is due, its open transactions and the VAT it comes to
// (so far, while it runs).
//
//	Q2 2026 changed   Q3 2026 open        Q4 2026 running
//	2.031,75 €        due 10.11. · 4 open  612,40 € so far
type SpanCard struct {
	metrics.VATSpan
	State  SpanState
	Due    time.Time // the return's deadline
	Open   int       // open business transactions
	Amount float64   // VAT due
}

// Case is one open transaction with the model's reading, if asked, and
// the Paperless scans fitting a new expense.
type Case struct {
	metrics.BankCase
	Guess  *assist.Guess
	Scans  []receipts.Candidate
	Number string            // a new expense's invoice number: the proposed scan's
	Note   string            // its public note: the booking's text
	parts  []Part            // the parts it counts (partsOf)
	gaps   []Part            // what the expense found or filled lacks
	lacks  map[string][]Part // each candidate expense's gaps, by key
}

// Account is a bank account of Invoice Ninja or Sure and its role.
type Account struct {
	ConnID        int64
	Service       enums.ServiceType
	Key, Name     string
	Role, Suggest sources.AccountRole // Suggest: what the kind implies while unset
}

// Ledger is what a new expense can choose from.
type Ledger struct {
	Vendors    []string
	Categories []sources.NinjaCategory
	TaxRates   []sources.NinjaTaxRate
}

// book is the loaded data of one check.
type book struct {
	scope    workbench.Scope
	choose   bool // several scopes, none picked
	conn     *model.Connection
	bankConn *model.Connection
	ninja    *sources.NinjaDataset
	bank     *sources.BankData
	settings map[string]any
	state    state
	cats     sources.Categories // the Verbund's mappings (verbund → categories)
	// numberSlot is the invoice number's custom field (receipts), 0 none;
	// scanSlot the scan's, 0 without Paperless paired.
	numberSlot, scanSlot int
}

// Open builds the page for Verbund scope ("": the last or only one) and
// period key ("" = the last finished period).
func Open(ctx context.Context, d *sql.DB, who *access.Principal, scope, spanKey string, today time.Time) (Page, error) {
	var p Page
	b, scopes, err := load(ctx, d, who, scope, svcdata.Stored)
	p.Scopes, p.Scope, p.Choose = scopes, b.scope.Key, b.choose
	if err != nil {
		return p, err
	}
	p.Conn, p.URL, p.Currency, p.Assist = b.conn.ID, b.conn.URL, b.ninja.Currency, assist.Enabled()
	if b.bankConn != nil {
		p.Bank = b.bankConn.Name
	}
	p.Interval, p.Method = metrics.TaxVATInterval(b.settings), metrics.TaxVATMethod(b.settings)
	p.Spans = metrics.VATSpans(today, p.Interval, spansShown)
	p.Span = defaultSpan(p.Spans, b.settings, today)
	if s, ok := metrics.VATSpanOf(spanKey, p.Interval); ok {
		p.Span = s
	}
	p.Ledger = Ledger{Categories: b.ninja.Categories, TaxRates: b.ninja.TaxRates}
	for _, v := range b.ninja.Vendors {
		p.Ledger.Vendors = append(p.Ledger.Vendors, v.Name)
	}
	if !b.ninja.BankRead {
		return p, ErrNoBank
	}

	in := b.inputs(p.Span, p.Method, today)
	p.Cards = b.cards(p.Spans, p.Method, today)
	p.Accounts = b.accounts()
	p.SetUp = slices.ContainsFunc(p.Accounts, func(a Account) bool { return a.Role != sources.RoleUnknown && a.Role != sources.RoleIgnored })
	p.CanSet, p.SpaceID = b.canSet(d, who), b.conn.SpaceID
	cases := metrics.BankCases(in)
	p.Check = metrics.VATCheckOf(in, cases)
	p.Elster = metrics.ElsterOf(b.ninja, p.Span.Start, p.Span.End, p.Method)
	if filed, ok := b.state.Filed[connKey(b.conn.ID)][p.Span.Key]; ok {
		p.Filed, p.Changed = true, p.Elster.Changed(filed)
	}
	if slot := b.cats.EuerSlot; slot > 0 {
		p.Euer, p.EuerLines = b.ninja.ExpenseFields[slot-1].Label, metrics.EuerChoices(b.ninja, slot)
	}
	if b.numberSlot > 0 {
		p.Number, p.NumberOf = true, b.ninja.ExpenseFields[b.numberSlot-1].Label
	}
	parts := partsOf(p.Euer != "", p.Number, b.scanSlot > 0)
	vendorEuer := metrics.VendorEuer(b.ninja, b.cats.EuerSlot)
	guesses := b.state.guesses(b.conn.ID)
	for _, c := range cases {
		g, ok := guesses[c.Txn.Key]
		cs := Case{BankCase: c, parts: parts}
		b.mapped(&cs)
		if ok {
			cs.Guess = &g
			p.Guessed = true
			b.apply(&cs)
		}
		cs.Draft.Euer = vendorEuer[cs.Draft.VendorKey]
		if cs.Txn.Debit {
			cs.Note = noteOf(cs.Txn)
		}
		b.gapped(&cs)
		b.propose(ctx, d, who, &cs)
		if cs.Waiting() {
			p.Cases = append(p.Cases, cs)
		} else {
			p.Apart = append(p.Apart, cs)
		}
	}
	p.Gaps = b.gapCases(ctx, d, who, p.Span, parts, vendorEuer)
	p.Docs = b.docCases(ctx, d, who, p.Span, parts, vendorEuer, proposed(p.Cases))
	p.Receipts = -1
	if c, err := receipts.CountsOf(ctx, d, who, receiptsTab, p.Span.Start.Year(), p.Span.Key); err == nil && c.Ready {
		p.Receipts = c.Match
	}
	for _, t := range b.ninja.BankTxns {
		if t.Archived && inSpan(t.Date, p.Span) {
			p.Skipped = append(p.Skipped, t)
		}
	}
	return p, nil
}

// returnDays is when a return is due after its period ends, without
// tax settings: the 10th of the next month.
const returnDays = 10

// defaultSpan is the last finished period through the day its return is
// due, then the running one: September's return is worked on until and
// on 10 October.
func defaultSpan(spans []metrics.VATSpan, settings map[string]any, today time.Time) metrics.VATSpan {
	last := spans[1]
	if !today.Before(returnDue(settings, last).AddDate(0, 0, 1)) {
		return spans[0]
	}
	return last
}

// returnDue is when s's return is due: by the tax settings, else the
// 10th after it ends.
func returnDue(settings map[string]any, s metrics.VATSpan) time.Time {
	if tax, ok := metrics.ParseTaxSettings(settings); ok {
		if d, found := metrics.VATReturnFor(tax, s.End); found {
			return d.Due
		}
	}
	return s.End.AddDate(0, 0, returnDays)
}

// cards checks every period of spans (newest first), oldest first: the
// same check as the page's, so the chosen card and the page agree.
func (b book) cards(spans []metrics.VATSpan, method string, today time.Time) []SpanCard {
	out := make([]SpanCard, 0, len(spans))
	for i := len(spans) - 1; i >= 0; i-- {
		s := spans[i]
		in := b.inputs(s, method, today)
		check := metrics.VATCheckOf(in, metrics.BankCases(in))
		c := SpanCard{VATSpan: s, Due: returnDue(b.settings, s), Open: check.Open, Amount: check.Due}
		c.State = b.spanState(c, method, today)
		out = append(out, c)
	}
	return out
}

// spanState says where c stands; a period and its deadline run through
// their last day.
func (b book) spanState(c SpanCard, method string, today time.Time) SpanState {
	if today.Before(c.End.AddDate(0, 0, 1)) {
		return SpanRunning
	}
	filed, ok := b.state.Filed[connKey(b.conn.ID)][c.Key]
	if ok && len(metrics.ElsterOf(b.ninja, c.Start, c.End, method).Changed(filed)) > 0 {
		return SpanChanged
	}
	if ok {
		return SpanFiled
	}
	if !today.Before(c.Due.AddDate(0, 0, 1)) {
		return SpanUnfiled
	}
	return SpanOpen
}

// mapped takes the category of a draft Sure named from the Verbund's
// mapping (Verbund → Kategorien) instead of by name; a vendor's own
// history stays first.
func (b book) mapped(c *Case) {
	if c.Sure == nil || b.bankConn == nil || c.Draft.From == metrics.FromHistory {
		return
	}
	key := b.cats.Sure[b.bankConn.ID][c.Sure.Category]
	if name := categoryName(b.ninja, key); name != "" {
		c.Draft.Category, c.Draft.CategoryKey, c.Draft.From = name, key, metrics.FromSure
	}
}

// categoryName is a Ninja category's name, "" if unknown.
func categoryName(n *sources.NinjaDataset, key string) string {
	for _, c := range n.Categories {
		if c.Key == key {
			return c.Name
		}
	}
	return ""
}

// apply lets the model's reading fill what the data left open; it never
// replaces a vendor's own history.
func (b book) apply(c *Case) {
	g := c.Guess
	if g.Private && c.Action != metrics.ActionLink {
		c.Action = metrics.ActionSkip
		return
	}
	if c.Private && c.Action == metrics.ActionNone {
		// Paid privately, but the model reads it as business.
		c.Action = metrics.ActionCreate
	}
	if !c.Txn.Debit || c.Draft.From == metrics.FromHistory {
		return
	}
	if c.Draft.VendorKey == "" && g.Vendor != "" {
		c.Draft.Vendor = g.Vendor
		for _, v := range b.ninja.Vendors {
			if equalFold(v.Name, g.Vendor) {
				c.Draft.VendorKey = v.Key
			}
		}
	}
	if c.Draft.CategoryKey == "" {
		for _, cat := range b.ninja.Categories {
			if equalFold(cat.Name, g.Category) {
				c.Draft.Category, c.Draft.CategoryKey = cat.Name, cat.Key
			}
		}
	}
	if !c.Draft.TaxKnown && g.TaxKnown {
		c.Draft.TaxRate, c.Draft.TaxKnown = g.TaxRate, true
	}
	c.Draft.From = metrics.FromModel
}

func inSpan(day string, s metrics.VATSpan) bool {
	d, ok := metrics.ParseDay(day)
	return ok && !d.Before(s.Start) && !d.After(s.End)
}

func (b book) inputs(s metrics.VATSpan, method string, today time.Time) metrics.VATInputs {
	return metrics.VATInputs{Ninja: b.ninja, Sure: b.bank, Start: s.Start, End: s.End, Method: method, Today: today}
}

// accounts lists Invoice Ninja's bank accounts, then Sure's.
func (b book) accounts() []Account {
	var out []Account
	for _, a := range b.ninja.BankAccounts {
		out = append(out, Account{ConnID: b.conn.ID, Service: enums.ServiceInvoiceNinja, Key: a.Key, Name: a.Name, Role: a.Role})
	}
	if b.bank != nil {
		for _, a := range slices.Concat(b.bank.Accounts, b.bank.Ignored) {
			out = append(out, Account{ConnID: b.bankConn.ID, Service: b.bank.From(), Key: a.ID, Name: a.Name, Role: a.Role, Suggest: a.Suggested()})
		}
	}
	return out
}

// canSet says whether the caller may set roles on both connections.
func (b book) canSet(d *sql.DB, who *access.Principal) bool {
	for _, c := range []*model.Connection{b.conn, b.bankConn} {
		if c == nil {
			continue
		}
		if v, err := connections.Get(d, who, c.ID); err != nil || v.Right < enums.RightManage {
			return false
		}
	}
	return true
}

// load picks the Verbund (workbench.Scopes): its Invoice Ninja, its
// Sure (or Firefly), their data and the space's tax settings.
func load(ctx context.Context, d *sql.DB, who *access.Principal, scope string, fresh svcdata.Freshness) (book, []workbench.Scope, error) {
	scopes, err := workbench.Scopes(d, who, need)
	if err != nil {
		return book{}, nil, err
	}
	if len(scopes) == 0 {
		return book{}, nil, ErrNoNinja
	}
	st, err := loadState(d, who)
	if err != nil {
		return book{}, scopes, err
	}
	if scope == "" {
		scope = st.Scope
	}

	b := book{state: st}
	b.scope, b.choose = workbench.Pick(scopes, scope)
	b.conn = b.scope.Conn(enums.ServiceInvoiceNinja)
	b.cats = sources.CategoriesOf(b.conn.Options)
	b.numberSlot = receipts.InvoiceSlotOf(b.conn)
	b.scanSlot = receipts.ScanSlotOf(d, who, b.conn.ID)
	for _, service := range need.Optional {
		if b.bankConn == nil {
			b.bankConn = b.scope.Conn(service)
		}
	}
	err = db.WithRead(d, func(tx *sql.Tx) error {
		sp, err := content.Space(tx, b.conn.SpaceID)
		if err == nil && sp != nil {
			b.settings = sp.Settings
		}
		return err
	})
	if err != nil {
		return b, scopes, err
	}

	res, err := fetch(ctx, d, who, b.conn, fresh)
	if err != nil {
		return b, scopes, err
	}
	if res.Error != "" {
		return b, scopes, FetchError{res.Error}
	}
	if b.ninja, _ = res.Data.(*sources.NinjaDataset); b.ninja == nil {
		return b, scopes, ErrNoBank
	}
	if b.bankConn != nil {
		if res, err := fetch(ctx, d, who, b.bankConn, fresh); err == nil {
			b.bank, _ = res.Data.(*sources.BankData)
		}
	}
	return b, scopes, nil
}

// fetch reads a connection's dataset; a stored one the background run
// has not fetched yet is fetched now.
func fetch(ctx context.Context, d *sql.DB, who *access.Principal, conn *model.Connection, fresh svcdata.Freshness) (svcdata.Result, error) {
	key, holder := sources.DataKey(enums.ServiceType(conn.Service)), model.UserHolder(who.UserID)
	res, err := svcdata.Get(ctx, d, key, nil, conn, holder, fresh)
	if err == nil && res.Pending {
		return svcdata.Get(ctx, d, key, nil, conn, holder, svcdata.Cached)
	}
	return res, err
}

// FetchError is a service that could not be read, with its message.
type FetchError struct{ Msg string }

func (e FetchError) Error() string { return e.Msg }

// Search looks in Paperless for scans for transaction key's new expense
// of vendor, or for gap case key's expense: query in title and content,
// within the booking's year.
func Search(ctx context.Context, d *sql.DB, who *access.Principal, scope, key, vendor, query string) ([]receipts.Candidate, error) {
	b, _, err := load(ctx, d, who, scope, svcdata.Stored)
	if err != nil {
		return nil, err
	}
	if exp, ok := strings.CutPrefix(key, fillPrefix); ok {
		j := slices.IndexFunc(b.ninja.Expenses, func(e sources.NinjaExpense) bool { return e.Key == exp })
		if j < 0 {
			return nil, nil
		}
		e := b.ninja.Expenses[j]
		return receipts.FindScans(ctx, d, who, b.conn.ID, e.Amount, e.Date, b.vendorName(e.VendorKey), query)
	}
	i := slices.IndexFunc(b.ninja.BankTxns, func(t sources.NinjaBankTxn) bool { return t.Key == key })
	if i < 0 {
		return nil, nil
	}
	t := b.ninja.BankTxns[i]
	return receipts.FindScans(ctx, d, who, b.conn.ID, t.Amount, t.Date, vendor, query)
}

// gapsOf lists what an expense lacks of the parts the page counts.
func (b book) gapsOf(e sources.NinjaExpense) []Part {
	var out []Part
	empty := func(slot int) bool { return slot > 0 && strings.TrimSpace(e.Custom[slot-1]) == "" }
	if e.CategoryKey == "" {
		out = append(out, PartCategory)
	}
	if empty(b.cats.EuerSlot) {
		out = append(out, PartEuer)
	}
	if empty(b.numberSlot) {
		out = append(out, PartNumber)
	}
	if empty(b.scanSlot) {
		out = append(out, PartScan)
	}
	if strings.TrimSpace(e.Notes) == "" {
		out = append(out, PartNote)
	}
	return out
}

// gapped notes what a link's expenses lack: the first's counts, being
// the one the page takes; the others' fields show when picked.
func (b book) gapped(c *Case) {
	c.lacks = map[string][]Part{}
	for _, e := range c.Expenses {
		c.lacks[e.Key] = b.gapsOf(e)
	}
	if len(c.Expenses) > 0 {
		c.gaps = c.lacks[c.Expenses[0].Key]
	}
}

// propose looks for the scans a case's expense may get, and takes the
// best one's invoice number.
func (b book) propose(ctx context.Context, d *sql.DB, who *access.Principal, cs *Case) {
	wants := cs.Act() == metrics.ActionCreate || cs.Offers(PartScan)
	if !cs.Counts() || b.scanSlot == 0 || !wants {
		return
	}
	cs.Scans, _ = receipts.ScansFor(ctx, d, who, b.conn.ID, cs.Txn.Amount, cs.Txn.Date, cs.Draft.Vendor)
	if cs.Scan() != 0 && b.numberSlot > 0 {
		cs.Number = cs.Scans[0].Number
	}
}

// gapCases are the period's linked expenses still without EÜR line or
// scan, each as a case to fill: "x-<key>", the vendor as payer.
func (b book) gapCases(ctx context.Context, d *sql.DB, who *access.Principal, s metrics.VATSpan, parts []Part, vendorEuer map[string]string) []Case {
	var out []Case
	for _, e := range b.ninja.Expenses {
		if e.TxnKey == "" || !inSpan(e.Date, s) {
			continue
		}
		gaps := b.gapsOf(e)
		if !slices.Contains(gaps, PartEuer) && !slices.Contains(gaps, PartScan) {
			continue
		}
		vendor := b.vendorName(e.VendorKey)
		txn := sources.NinjaBankTxn{Key: fillPrefix + e.Key, Date: e.Date, Payer: vendor, Description: e.Number, Amount: e.Amount, Debit: true}
		cs := Case{BankCase: metrics.BankCase{Txn: txn, Expenses: []sources.NinjaExpense{e}, Action: ActionFill,
			Draft: metrics.ExpenseDraft{Vendor: vendor, VendorKey: e.VendorKey, Euer: vendorEuer[e.VendorKey]}}, parts: parts}
		b.gapped(&cs)
		b.propose(ctx, d, who, &cs)
		out = append(out, cs)
	}
	return out
}

func (b book) vendorName(key string) string {
	for _, v := range b.ninja.Vendors {
		if v.Key == key {
			return v.Name
		}
	}
	return ""
}

// proposed are the scans the open cases propose: no receipt case for
// them, the booking takes them.
func proposed(cases []Case) map[int64]bool {
	out := map[int64]bool{}
	for _, c := range cases {
		if id := c.Scan(); id != 0 {
			out[id] = true
		}
	}
	return out
}

// docCases are the period's free scans no booking or expense fits, each
// as a new expense: "d-<id>", dressed as a booking of the scan's day,
// amount and correspondent.
func (b book) docCases(ctx context.Context, d *sql.DB, who *access.Principal, s metrics.VATSpan, parts []Part, vendorEuer map[string]string, taken map[int64]bool) []Case {
	if b.scanSlot == 0 {
		return nil
	}
	scans, _ := receipts.Unbooked(ctx, d, who, b.conn.ID, s.Start, s.End)
	var out []Case
	for _, sc := range scans {
		if taken[sc.Doc.ID] {
			continue
		}
		sc.Score = scanSure
		txn := docTxn(sc.Doc, sc.Amount)
		cs := Case{BankCase: metrics.BankCase{Txn: txn, Action: ActionReceipt, Draft: metrics.DraftOf(b.ninja, txn)},
			Scans: []receipts.Candidate{sc}, Note: strings.TrimSpace(sc.Doc.Title), parts: parts}
		cs.Draft.Euer = vendorEuer[cs.Draft.VendorKey]
		if b.numberSlot > 0 {
			cs.Number = sc.Number
		}
		out = append(out, cs)
	}
	return out
}

// scanSure is the score of a receipt case's own scan: it is the case.
const scanSure = 100

// docTxn dresses a scan as the booking it stands for.
func docTxn(doc sources.ReceiptDoc, amount float64) sources.NinjaBankTxn {
	return sources.NinjaBankTxn{Key: docPrefix + strconv.FormatInt(doc.ID, 10), Date: doc.Day, Payer: cmp.Or(doc.Correspondent, doc.Title),
		Description: doc.Title, Amount: amount, Debit: true}
}
