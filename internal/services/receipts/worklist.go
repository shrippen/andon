package receipts

// Helpers of the page around the two lists:
//
//	CountsOf       what waits in each tab and year (the tab and year chips)
//	LinkMany       confirms several sure matches in one go
//	FindExpenses   the expenses a scan could belong to, searched by hand
//	FindCombos     1∶n combos of one expense on request
//	Draft, Create  a new Invoice Ninja expense from a scan, then linked

import (
	"context"
	"database/sql"
	"slices"
	"sort"
	"strconv"
	"strings"

	"andon/internal/outbound"
	"andon/internal/services/access"
	"andon/internal/sources"
)

// Counts is what waits where: unlinked expenses per year, tagged scans
// without expense, linked expenses and hidden entries of the year.
type Counts struct {
	Ready                         bool
	Match, Queue, Linked, Ignored int
	Years                         map[int]int
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

// CountsOf counts for the year. Reads come from the cache the tabs fill.
func CountsOf(ctx context.Context, d *sql.DB, who *access.Principal, year int) (Counts, error) {
	p, err := openPair(d, who)
	if err != nil || !p.mapping.Complete() {
		return Counts{}, err
	}
	out := Counts{Ready: true, Years: map[int]int{}}
	set, err := p.expenses(ctx, d, who)
	if err != nil {
		return Counts{}, err
	}
	hiddenE, hiddenD := p.state.ignored(KindExpense, p.ninja.ID), p.state.ignored(KindDoc, p.docs.ID)
	out.Ignored = len(hiddenE) + len(hiddenD)
	for _, e := range p.mapping.unlinkedExpenses(set.Expenses) {
		if y, err := strconv.Atoi(e.Day[:min(len(e.Day), 4)]); err == nil && !slices.Contains(hiddenE, e.Key) {
			out.Years[y]++
		}
	}
	out.Match = out.Years[year]
	for _, e := range inYear(set.Expenses, year) {
		if e.Custom[p.mapping.LinkSlot-1] != "" {
			out.Linked++
		}
	}
	if p.mapping.QueueTag == "" {
		return out, nil
	}
	docs, err := p.docSet(ctx, d, who, year)
	if err != nil {
		return out, nil // the queue tab says why
	}
	tag, found := docs.Tags[strings.ToLower(p.mapping.QueueTag)]
	for _, doc := range p.mapping.unlinkedDocs(docs.Docs) {
		if found && slices.Contains(doc.Tags, tag) && !slices.Contains(hiddenD, strconv.FormatInt(doc.ID, 10)) {
			out.Queue++
		}
	}
	return out, nil
}

// Pair is one expense with the scans to link to it.
type Pair struct {
	Expense string
	Docs    []int64
}

// LinkMany links pair after pair and stops at the first error; it
// returns how many it linked.
func LinkMany(ctx context.Context, d *sql.DB, who *access.Principal, pairs []Pair, ip string) (int, error) {
	for i, pr := range pairs {
		if _, err := Link(ctx, d, who, pr.Expense, pr.Docs, ip); err != nil {
			return i, err
		}
	}
	return len(pairs), nil
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
