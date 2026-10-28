package receipts

// Helpers of the page around the two lists:
//
//	CountsOf       what waits in each tab and year (the tab and year chips)
//	FindExpenses   the expenses a scan could belong to, searched by hand
//	FindCombos     1∶n combos of one expense on request
//	Draft, Create  a new Invoice Ninja expense from a scan, then linked
//	ScansFor       the scans fitting an expense about to be created

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/outbound"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/sources"
)

// Counts is what waits where: the number of each tab's list for the
// year, and the open tab's list per year.
type Counts struct {
	Ready                         bool
	Match, Queue, Linked, Ignored int
	Years                         map[int]int
	AllYears                      bool // Years has every year; false: only the one asked for
}

// Count is one tab's number, -1 = unknown.
func (c Counts) Count(tab string) int {
	if !c.Ready {
		return -1
	}
	switch tab {
	case "match":
		return c.Match
	case "queue":
		return c.Queue
	case "linked":
		return c.Linked
	case "ignored":
		return c.Ignored
	}
	return -1
}

// Year is the open tab's number for one year, -1 = unknown.
func (c Counts) Year(year int) int {
	if n, ok := c.Years[year]; ok {
		return n
	}
	if c.Ready && c.AllYears {
		return 0
	}
	return -1
}

// CountsOf counts the lists of the tabs for the year, and the open tab's
// list per year, with the same filters the lists use (open, queued,
// linkedOf). Reads come from the cache the tabs fill.
func CountsOf(ctx context.Context, d *sql.DB, who *access.Principal, tab string, year int, span string) (Counts, error) {
	p, err := openPair(d, who)
	if err != nil || !p.mapping.Complete() {
		return Counts{}, err
	}
	out := Counts{Ready: true, Years: map[int]int{}}
	set, err := p.expenses(ctx, d, who)
	if err != nil {
		return Counts{}, err
	}
	out.Ignored = len(p.state.ignored(KindExpense, p.ninja.ID)) + len(p.state.ignored(KindDoc, p.docs.ID))
	open, linked := p.open(set.Expenses), p.mapping.linkedOf(set.Expenses)
	out.Match, out.Linked = len(inSpan(inYear(open, year), span)), len(inYear(linked, year))
	switch tab {
	case "match":
		out.Years, out.AllYears = perYear(open), true
	case "linked":
		out.Years, out.AllYears = perYear(linked), true
	}

	docs, err := p.docSet(ctx, d, who, year)
	if err != nil {
		return out, nil // the queue tab says why
	}
	queued, _ := p.queued(docs, p.free(docs.Docs))
	out.Queue = len(queued)
	if tab == "queue" {
		// Scans are read per year: only this one is known.
		out.Years[year] = out.Queue
	}
	return out, nil
}

// perYear counts expenses by the year of their day; undated ones drop out.
func perYear(list []sources.ReceiptExpense) map[int]int {
	out := map[int]int{}
	for _, e := range list {
		if y, err := strconv.Atoi(e.Day[:min(len(e.Day), 4)]); err == nil {
			out[y]++
		}
	}
	return out
}

// ExpenseSearch is the expenses a scan could belong to.
type ExpenseSearch struct {
	Links
	Doc   sources.ReceiptDoc
	Query string
	Hits  []ExpenseHit
}

// FindExpenses scores the year's unlinked expenses matching query
// against one scan, best first.
func FindExpenses(ctx context.Context, d *sql.DB, who *access.Principal, docID int64, year int, query string) (ExpenseSearch, error) {
	p, err := openPair(d, who)
	if err != nil {
		return ExpenseSearch{}, err
	}
	out := ExpenseSearch{Links: p.links(), Query: strings.TrimSpace(query)}
	if !p.mapping.Complete() {
		return out, ErrMapping
	}
	if out.Doc, err = p.doc(ctx, d, who, docID); err != nil {
		return out, err
	}
	expenses, _, err := p.unlinked(ctx, d, who, year)
	if err != nil {
		return out, err
	}
	x := p.matcher()
	for _, e := range expenses {
		if matchesQuery(e, out.Query) {
			out.Hits = append(out.Hits, ExpenseHit{Expense: e, Candidate: x.score(e, out.Doc)})
		}
	}
	sort.SliceStable(out.Hits, func(i, j int) bool { return out.Hits[i].Score > out.Hits[j].Score })
	out.Hits = out.Hits[:min(len(out.Hits), expensesShown)]
	return out, nil
}

func (p pair) doc(ctx context.Context, d *sql.DB, who *access.Principal, id int64) (sources.ReceiptDoc, error) {
	found, err := p.docsByID(ctx, d, who, []int64{id})
	if err != nil {
		return sources.ReceiptDoc{}, err
	}
	if found[id] == nil {
		return sources.ReceiptDoc{}, ErrNotFound
	}
	return *found[id], nil
}

// ComboView is the combos of one expense, looked for on request.
type ComboView struct {
	Links
	Expense sources.ReceiptExpense
	Year    int
	Combos  []Combo
}

// FindCombos looks for 1∶n combos of one expense among the year's
// unlinked scans, also where a single scan has the exact amount.
func FindCombos(ctx context.Context, d *sql.DB, who *access.Principal, expenseKey string, year int) (ComboView, error) {
	p, err := openPair(d, who)
	if err != nil {
		return ComboView{}, err
	}
	out := ComboView{Links: p.links(), Year: year}
	if !p.mapping.Complete() {
		return out, ErrMapping
	}
	set, err := p.expenses(ctx, d, who)
	if err != nil {
		return out, err
	}
	i := slices.IndexFunc(set.Expenses, func(e sources.ReceiptExpense) bool { return e.Key == expenseKey })
	if i < 0 {
		return out, ErrNotFound
	}
	out.Expense = set.Expenses[i]
	_, docs, err := p.unlinked(ctx, d, who, year)
	if err != nil {
		return out, err
	}
	out.Combos = p.matcher().combos(out.Expense, docs)
	return out, nil
}

// NewExpense is an expense to create from a scan.
type NewExpense struct {
	Amount             float64
	Day, Vendor, Notes string
}

// ExpenseDraft is the form for a new expense, filled from the scan.
type ExpenseDraft struct {
	Links
	Doc sources.ReceiptDoc
	NewExpense
}

// Draft fills a new expense from a scan: its single amount, date,
// correspondent and title.
func Draft(ctx context.Context, d *sql.DB, who *access.Principal, docID int64) (ExpenseDraft, error) {
	p, err := openPair(d, who)
	if err != nil {
		return ExpenseDraft{}, err
	}
	out := ExpenseDraft{Links: p.links()}
	if out.Doc, err = p.doc(ctx, d, who, docID); err != nil {
		return out, err
	}
	out.Amount, _ = p.matcher().comboAmount(out.Doc)
	out.Day, out.Vendor, out.Notes = docDay(out.Doc), out.Doc.Correspondent, out.Doc.Title
	return out, nil
}

// Create makes the expense in Invoice Ninja, with the vendor of that
// name if there is one, and links the scan to it.
func Create(ctx context.Context, d *sql.DB, who *access.Principal, docID int64, in NewExpense, ip string) (string, error) {
	p, err := openPair(d, who)
	if err != nil {
		return "", err
	}
	if err := connections.Writable(d, who, p.ninja.ID, p.docs.ID); err != nil {
		return "", err
	}
	if !p.mapping.Complete() {
		return "", ErrMapping
	}
	if sources.IsDemo(p.ninja.URL) || sources.IsDemo(p.docs.URL) {
		return "", ErrDemo
	}
	if in.Amount <= 0 {
		return "", ErrAmount
	}
	nctx, err := p.sourceCtx(d, who, p.ninja)
	if err != nil {
		return "", err
	}
	body := map[string]any{"amount": in.Amount, "date": in.Day, "public_notes": strings.TrimSpace(in.Notes)}
	vendor, err := sources.NinjaVendorKey(ctx, nctx, in.Vendor)
	if err != nil {
		return "", FetchError{err.Error()}
	}
	if vendor != "" {
		body["vendor_id"] = vendor
	}
	to := outbound.Target{URL: p.ninja.URL, Token: nctx.Secret, VerifyTLS: p.ninja.VerifyTLS}
	key, _, err := outbound.NinjaExpenseCreate(ctx, to, body)
	if err != nil {
		return "", FetchError{err.Error()}
	}
	return Link(ctx, d, who, key, []int64{docID}, ip)
}

// scansOffered is how many scans a new expense is offered.
const scansOffered = 3

// ScansFor are the free scans fitting an expense about to be created
// (the VAT check's "create expense"), best first, from a fair score up.
// Nothing when the user's receipts pair is another Invoice Ninja or its
// fields are not chosen: the link could not be written.
func ScansFor(ctx context.Context, d *sql.DB, who *access.Principal, ninjaID int64, amount float64, day, vendor string) ([]Candidate, error) {
	p, err := openPair(d, who)
	if errors.Is(err, ErrNoPair) || (err == nil && (p.ninja.ID != ninjaID || !p.mapping.Complete())) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	year, err := strconv.Atoi(day[:min(len(day), len("2006"))])
	if err != nil {
		return nil, nil
	}
	docs, err := p.docSet(ctx, d, who, year)
	if err != nil {
		return nil, err
	}

	x, e := p.matcher(), sources.ReceiptExpense{Amount: amount, Day: day, Vendor: vendor}
	var out []Candidate
	for _, doc := range p.free(docs.Docs) {
		if c := x.score(e, doc); c.Score >= levelMid {
			c.Number = docNumber(p.mapping, doc)
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out[:min(len(out), scansOffered)], nil
}

// FindScans searches Paperless for free scans for a new expense of the
// VAT check: query in title and content, within the year of day; best
// first. Without receipts set up for that Invoice Ninja it finds none.
func FindScans(ctx context.Context, d *sql.DB, who *access.Principal, ninjaID int64, amount float64, day, vendor, query string) ([]Candidate, error) {
	p, err := openPair(d, who)
	if errors.Is(err, ErrNoPair) || (err == nil && (p.ninja.ID != ninjaID || !p.mapping.Complete())) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	year, err := strconv.Atoi(day[:min(len(day), len("2006"))])
	if err != nil || strings.TrimSpace(query) == "" {
		return nil, nil
	}
	sctx, err := p.sourceCtx(d, who, p.docs)
	if err != nil {
		return nil, err
	}
	e := sources.ReceiptExpense{Amount: amount, Day: day, Vendor: vendor}
	found, err := sources.PaperlessSearch(ctx, sctx, searchOf(e, SearchInput{Query: query, Year: year}, p.mapping))
	if err != nil {
		return nil, FetchError{err.Error()}
	}

	x := p.matcher()
	var out []Candidate
	for _, doc := range p.free(found) {
		c := x.score(e, doc)
		c.Number = docNumber(p.mapping, doc)
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out[:min(len(out), scansFound)], nil
}

// scansFound is how many hits FindScans offers.
const scansFound = 8

// docNumber is the invoice number Paperless holds for doc, "" none.
func docNumber(m Mapping, doc sources.ReceiptDoc) string {
	n, _ := doc.Custom[m.FieldInvoice].(string)
	return strings.TrimSpace(n)
}

// Unbooked are the free scans of [from, to] carrying the queue tag that
// no expense without a scan fits: paid in cash or privately, they wait
// for an expense of their own (the VAT check's "receipts without an
// expense"). Nothing for another Invoice Ninja than the pair's.
func Unbooked(ctx context.Context, d *sql.DB, who *access.Principal, ninjaID int64, from, to time.Time) ([]Candidate, error) {
	p, err := openPair(d, who)
	if errors.Is(err, ErrNoPair) || (err == nil && (p.ninja.ID != ninjaID || !p.mapping.Complete())) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	set, err := p.docSet(ctx, d, who, from.Year())
	if err != nil {
		return nil, err
	}
	_, docs, err := p.unlinked(ctx, d, who, from.Year())
	if err != nil {
		return nil, err
	}

	// An expense of the neighbour year may hold a scan at the year's edge.
	all, err := p.expenses(ctx, d, who)
	if err != nil {
		return nil, err
	}
	expenses := aroundYear(p.open(all.Expenses), from.Year())
	tagged, found := p.queued(set, docs)
	if !found {
		return nil, nil
	}

	out := unclaimed(p.matcher(), tagged, expenses, docs, from, to)
	for i := range out {
		out[i].Number = docNumber(p.mapping, out[i].Doc)
	}
	return out, nil
}

// unclaimed are the tagged scans of [from, to] no expense fits: no
// combo and no hit from levelMid on.
func unclaimed(x matcher, tagged []sources.ReceiptDoc, expenses []sources.ReceiptExpense, pool []sources.ReceiptDoc, from, to time.Time) []Candidate {
	var out []Candidate
	for _, m := range x.Reverse(tagged, expenses, pool) {
		day, err := time.Parse(time.DateOnly, docDay(m.Doc))
		fits := len(m.Combos) > 0 || (len(m.Hits) > 0 && m.Hits[0].Score >= levelMid)
		if err != nil || fits || day.Before(from) || day.After(to) {
			continue
		}
		m.Doc.Day = day.Format(time.DateOnly)
		out = append(out, Candidate{Doc: m.Doc, Amount: m.Amount})
	}
	return out
}
