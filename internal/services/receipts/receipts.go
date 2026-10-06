// Package receipts links Invoice Ninja expenses to their Paperless scans
// (what PaperNinja did on its own):
//
//	Setup     the Invoice Ninja and Paperless connections the user can use;
//	          several of either → the user picks, the pick is remembered
//	Suggest   unlinked expenses of a year → the scans that fit best
//	Queue     scans tagged "waiting" → the expenses that fit best
//	Link      expense custom value ← scan URL(s); scan fields ← expense
//	          number, expense URL, invoice number. If the scan side
//	          fails, the expense side is rolled back.
//
// Where the link lives on both sides is stored on the connections
// (options receipt_*), so everybody using them shares one mapping.
package receipts

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/services/verbund"
	"andon/internal/sources"
)

var (
	// ErrNoPair means the user has no Invoice Ninja or no Paperless to use,
	// or has not picked which.
	ErrNoPair = errors.New("receipts.err_no_pair")
	// ErrMapping means the fields that hold the link are not chosen yet.
	ErrMapping = errors.New("receipts.err_mapping")
	// ErrDemo means a demo connection, which takes no writes.
	ErrDemo = errors.New("receipts.err_demo")
	// ErrNotFound means the expense or a scan is gone.
	ErrNotFound = errors.New("receipts.err_not_found")
	// ErrManage means the user may not change the connections' mapping.
	ErrManage = errors.New("receipts.err_manage")
	// ErrAmount means a new expense without an amount.
	ErrAmount = errors.New("receipts.err_amount")
	// ErrFieldTwice means one field chosen for two parts of the link.
	ErrFieldTwice = errors.New("receipts.err_field_twice")
)

// LinkedElsewhere means a scan already names another expense; linking it
// again would leave that expense pointing at it, one-sided.
type LinkedElsewhere struct{ Number string }

func (e LinkedElsewhere) Error() string { return "receipts.err_linked_elsewhere" }

// checkFree refuses scans that name another expense than e.
func checkFree(m Mapping, e sources.ReceiptExpense, docs []sources.ReceiptDoc) error {
	for _, d := range docs {
		owner := linkedTo(m, d)
		if owner != "" && owner != e.Number && owner != e.Key {
			return LinkedElsewhere{Number: owner}
		}
	}
	return nil
}

// linkedTo is the expense number a scan names, "" if none.
func linkedTo(m Mapping, d sources.ReceiptDoc) string {
	if m.FieldExpense == 0 || !filled(d.Custom[m.FieldExpense]) {
		return ""
	}
	v, ok := d.Custom[m.FieldExpense].(string)
	if !ok {
		return "?"
	}
	return strings.TrimSpace(v)
}

// FetchError is a service that could not be read, with its message.
type FetchError struct{ Msg string }

func (e FetchError) Error() string { return e.Msg }

// Connection option keys of the mapping.
const (
	optInvoiceSlot  = "receipt_invoice_slot"
	optLinkSlot     = "receipt_link_slot"
	optFieldInvoice = "receipt_field_invoice"
	optFieldExpense = "receipt_field_expense"
	optFieldLink    = "receipt_field_link"
	optFieldAmount  = "receipt_field_amount"
	optQueueTag     = "receipt_queue_tag"
)

const (
	expenseSource = "ninja.expenses"
	docSource     = "paperless.docs"
	linkedShown   = 100
	expensesShown = 10
	urlSeparator  = " "
)

// ── connections ──

// Choice is one connection the user may use here. Label is what the
// page shows: the space alone when all of a kind share one name, else
// the name, with the space where names repeat.
type Choice struct {
	ID                 int64
	Name, Space, Label string
	NeedsToken         bool // personal credentials, the user has not stored his
}

// label names each choice within its list, without saying twice what
// the list's heading says ("Paperless" under "Paperless-ngx").
func label(list []Choice) {
	same, count := true, map[string]int{}
	for _, c := range list {
		count[c.Name]++
		same = same && c.Name == list[0].Name
	}
	for i := range list {
		c := &list[i]
		switch {
		case same && c.Space != "":
			c.Label = c.Space
		case count[c.Name] > 1 && c.Space != "":
			c.Label = c.Name + " · " + c.Space
		default:
			c.Label = c.Name
		}
	}
}

// Setup is what the user can pick from and what is picked.
type Setup struct {
	Ninjas, Docs     []Choice
	Ninja, Paperless int64 // picked; 0 = nothing yet
	Pick             bool  // several to choose from and nothing picked yet
}

// Ready tells whether both sides are picked.
func (s Setup) Ready() bool { return s.Ninja != 0 && s.Paperless != 0 }

// Several tells whether there is anything to switch between.
func (s Setup) Several() bool { return len(s.Ninjas) > 1 || len(s.Docs) > 1 }

// SetupOf lists the usable connections and the user's pick. A single
// usable connection on both sides is picked by itself.
func SetupOf(d *sql.DB, who *access.Principal) (Setup, error) {
	views, err := connections.Listing(d, who, enums.RightUse)
	if err != nil {
		return Setup{}, err
	}
	var out Setup
	for _, v := range views {
		c := Choice{ID: v.ID, Name: v.Name, Space: who.Spaces[v.SpaceID].Name, NeedsToken: v.Mode == enums.CredentialPersonal && !v.HasMine}
		switch v.Service {
		case enums.ServiceInvoiceNinja:
			out.Ninjas = append(out.Ninjas, c)
		case enums.ServicePaperless:
			out.Docs = append(out.Docs, c)
		}
	}
	label(out.Ninjas)
	label(out.Docs)
	s, err := loadState(d, who)
	if err != nil {
		return Setup{}, err
	}
	out.Ninja, out.Paperless = pickOf(out.Ninjas, s.Ninja), pickOf(out.Docs, s.Paperless)
	savedPick := out.Ninja != 0 && out.Paperless != 0
	if !savedPick {
		out.Ninja, out.Paperless = only(out.Ninjas), only(out.Docs)
		if err := fromVerbund(d, who, &out); err != nil {
			return Setup{}, err
		}
		out.Pick = len(out.Ninjas) > 0 && len(out.Docs) > 0 && (out.Ninja == 0 || out.Paperless == 0)
	}
	return out, nil
}

// fromVerbund completes a half pick with the other side's partner: the
// one Ninja's Paperless, or the one Paperless's Ninja.
func fromVerbund(d *sql.DB, who *access.Principal, s *Setup) error {
	complete := func(have int64, service enums.ServiceType, list []Choice) (int64, error) {
		conn, err := connections.ByID(d, have)
		if err != nil || conn == nil {
			return 0, err
		}
		partner, _, err := verbund.Partner(d, who, verbund.Asker{SpaceID: conn.SpaceID, ConnID: conn.ID}, service)
		if err != nil || partner == nil {
			return 0, err
		}
		return pickOf(list, partner.ID), nil
	}
	var err error
	switch {
	case s.Ninja != 0 && s.Paperless == 0:
		s.Paperless, err = complete(s.Ninja, enums.ServicePaperless, s.Docs)
	case s.Paperless != 0 && s.Ninja == 0:
		s.Ninja, err = complete(s.Paperless, enums.ServiceInvoiceNinja, s.Ninjas)
	}
	return err
}

// pickOf keeps a saved pick that is still usable.
func pickOf(list []Choice, id int64) int64 {
	for _, c := range list {
		if c.ID == id && !c.NeedsToken {
			return id
		}
	}
	return 0
}

// only is the single usable connection of a list, 0 if none or several.
func only(list []Choice) int64 {
	var found int64
	for _, c := range list {
		if c.NeedsToken {
			continue
		}
		if found != 0 {
			return 0
		}
		found = c.ID
	}
	return found
}

// Choose remembers which Invoice Ninja and Paperless the user works with.
func Choose(d *sql.DB, who *access.Principal, ninja, paperless int64) error {
	setup, err := SetupOf(d, who)
	if err != nil {
		return err
	}
	if pickOf(setup.Ninjas, ninja) == 0 || pickOf(setup.Docs, paperless) == 0 {
		return ErrNoPair
	}
	return saveState(d, who, func(s *state) { s.Ninja, s.Paperless = ninja, paperless })
}

// pair is the picked connections with their mapping.
type pair struct {
	ninja, docs *model.Connection
	mapping     Mapping
	state       state
}

func openPair(d *sql.DB, who *access.Principal) (pair, error) {
	setup, err := SetupOf(d, who)
	if err != nil {
		return pair{}, err
	}
	if !setup.Ready() {
		return pair{}, ErrNoPair
	}
	var p pair
	for _, id := range []int64{setup.Ninja, setup.Paperless} {
		if _, err := connections.Get(d, who, id); err != nil {
			return pair{}, err
		}
	}
	if p.ninja, err = connections.ByID(d, setup.Ninja); err != nil {
		return pair{}, err
	}
	if p.docs, err = connections.ByID(d, setup.Paperless); err != nil {
		return pair{}, err
	}
	if p.ninja == nil || p.docs == nil {
		return pair{}, ErrNoPair
	}
	p.mapping = mappingOf(p.ninja, p.docs)
	p.state, err = loadState(d, who)
	return p, err
}

// mappingOf reads the mapping from both connections' options; demo
// connections come mapped.
func mappingOf(ninja, docs *model.Connection) Mapping {
	nOpts, dOpts := ninja.Options, docs.Options
	if sources.IsDemo(ninja.URL) && sources.IsDemo(docs.URL) && nOpts[optLinkSlot] == nil {
		nOpts, dOpts = sources.DemoReceiptOptions()
	}
	num := func(opts map[string]any, key string) int64 {
		switch v := opts[key].(type) {
		case float64:
			return int64(v)
		case int:
			return int64(v)
		case int64:
			return v
		case string:
			n, _ := strconv.ParseInt(v, 10, 64)
			return n
		}
		return 0
	}
	slot := func(key string) int {
		n := int(num(nOpts, key))
		if n < 1 || n > sources.NinjaSlots {
			return 0
		}
		return n
	}
	tag, _ := dOpts[optQueueTag].(string)
	return Mapping{
		InvoiceSlot: slot(optInvoiceSlot), LinkSlot: slot(optLinkSlot),
		FieldInvoice: num(dOpts, optFieldInvoice), FieldExpense: num(dOpts, optFieldExpense),
		FieldLink: num(dOpts, optFieldLink), FieldAmount: num(dOpts, optFieldAmount), QueueTag: strings.TrimSpace(tag),
	}
}

func (p pair) matcher() matcher { return matcher{mapping: p.mapping, aliases: p.state.Aliases} }

// ── reading ──

func (p pair) expenses(ctx context.Context, d *sql.DB, who *access.Principal) (*sources.ExpenseSet, error) {
	uid := who.UserID
	res, err := svcdata.Get(ctx, d, expenseSource, nil, p.ninja, model.UserHolder(uid), svcdata.Cached)
	if err != nil {
		return nil, err
	}
	set, ok := res.Data.(*sources.ExpenseSet)
	if res.Error != "" || !ok {
		return nil, FetchError{res.Error}
	}
	return set, nil
}

func (p pair) docSet(ctx context.Context, d *sql.DB, who *access.Principal, year int) (*sources.DocSet, error) {
	uid := who.UserID
	res, err := svcdata.Get(ctx, d, docSource, map[string]any{"year": year}, p.docs, model.UserHolder(uid), svcdata.Cached)
	if err != nil {
		return nil, err
	}
	set, ok := res.Data.(*sources.DocSet)
	if res.Error != "" || !ok {
		return nil, FetchError{res.Error}
	}
	return set, nil
}

func (p pair) sourceCtx(d *sql.DB, who *access.Principal, conn *model.Connection) (sources.Ctx, error) {
	return svcdata.SourceCtx(d, conn, model.UserHolder(who.UserID))
}

// Links are the addresses a page links to.
type Links struct{ NinjaURL, DocsURL string }

// ExpenseURL is an expense's edit page in Invoice Ninja.
func (l Links) ExpenseURL(key string) string { return l.NinjaURL + "/expenses/" + key + "/edit" }

// DocURL is a scan's page in Paperless.
func (l Links) DocURL(id int64) string {
	return l.DocsURL + "/documents/" + strconv.FormatInt(id, 10) + "/"
}

func (p pair) links() Links {
	return Links{NinjaURL: strings.TrimRight(p.ninja.URL, "/"), DocsURL: strings.TrimRight(p.docs.URL, "/")}
}

// Suggestions is the expense-first view.
type Suggestions struct {
	Links
	Mapping        Mapping
	Year           int
	Matches        []ExpenseMatch
	Expenses, Docs int
}

// Suggest lists the year's unlinked expenses with their best scans.
func Suggest(ctx context.Context, d *sql.DB, who *access.Principal, year int) (Suggestions, error) {
	p, err := openPair(d, who)
	if err != nil {
		return Suggestions{}, err
	}
	out := Suggestions{Links: p.links(), Mapping: p.mapping, Year: year}
	if !p.mapping.Complete() {
		return out, ErrMapping
	}
	expenses, docs, err := p.unlinked(ctx, d, who, year)
	if err != nil {
		return out, err
	}
	out.Expenses, out.Docs = len(expenses), len(docs)
	out.Matches = p.matcher().Matches(expenses, docs)
	return out, nil
}

// unlinked reads the year's unlinked, not ignored expenses and scans,
// learning vendor names from old links the first time.
func (p *pair) unlinked(ctx context.Context, d *sql.DB, who *access.Principal, year int) ([]sources.ReceiptExpense, []sources.ReceiptDoc, error) {
	expenses, err := p.expenses(ctx, d, who)
	if err != nil {
		return nil, nil, err
	}
	docs, err := p.docSet(ctx, d, who, year)
	if err != nil {
		return nil, nil, err
	}
	p.backfill(ctx, d, who, expenses.Expenses)

	hiddenE, hiddenD := p.state.ignored(KindExpense, p.ninja.ID), p.state.ignored(KindDoc, p.docs.ID)
	open := slices.DeleteFunc(inYear(p.mapping.unlinkedExpenses(expenses.Expenses), year), func(e sources.ReceiptExpense) bool {
		return slices.Contains(hiddenE, e.Key)
	})
	scans := slices.DeleteFunc(p.mapping.unlinkedDocs(docs.Docs), func(doc sources.ReceiptDoc) bool {
		return slices.Contains(hiddenD, strconv.FormatInt(doc.ID, 10))
	})
	return open, scans, nil
}

// backfill learns vendor names from links made before (once per pair).
// It only helps, so failures are ignored.
func (p *pair) backfill(ctx context.Context, d *sql.DB, who *access.Principal, expenses []sources.ReceiptExpense) {
	key := pairKey(p.ninja.ID, p.docs.ID)
	if slices.Contains(p.state.Backfilled, key) || p.mapping.LinkSlot == 0 {
		return
	}
	byDoc := map[int64]string{}
	var ids []int64
	for _, e := range expenses {
		if e.Vendor == "" {
			continue
		}
		for _, id := range docIDs(e.Custom[p.mapping.LinkSlot-1]) {
			byDoc[id] = e.Vendor
			ids = append(ids, id)
		}
	}
	sctx, err := p.sourceCtx(d, who, p.docs)
	if err != nil {
		return
	}
	docs, err := sources.PaperlessDocsByID(ctx, sctx, ids)
	if err != nil {
		return
	}
	for _, doc := range docs {
		p.state.Aliases.learn(byDoc[doc.ID], doc.Correspondent)
	}
	learned := p.state.Aliases
	err = saveState(d, who, func(s *state) {
		for _, group := range learned {
			for _, name := range group[1:] {
				s.Aliases.learn(group[0], name)
			}
		}
		s.Backfilled = append(s.Backfilled, key)
	})
	if err != nil {
		slog.Warn("receipts: keep learned vendor names", "err", err)
	}
}

var docIDPattern = regexp.MustCompile(`/documents/(\d+)`)

// docIDs reads the scan ids of a link value ("…/documents/11/ …/documents/12/").
func docIDs(value string) []int64 {
	var out []int64
	for _, m := range docIDPattern.FindAllStringSubmatch(value, -1) {
		if id, err := strconv.ParseInt(m[1], 10, 64); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// QueueView is the scan-first view.
type QueueView struct {
	Links
	Mapping        Mapping
	Year           int
	TagFound       bool
	Matches        []DocMatch
	Docs, Expenses int
}

// Queue lists the year's unlinked scans carrying the queue tag with
// their best expenses.
func Queue(ctx context.Context, d *sql.DB, who *access.Principal, year int) (QueueView, error) {
	p, err := openPair(d, who)
	if err != nil {
		return QueueView{}, err
	}
	out := QueueView{Links: p.links(), Mapping: p.mapping, Year: year}
	if !p.mapping.Complete() {
		return out, ErrMapping
	}
	if p.mapping.QueueTag == "" {
		return out, nil
	}
	set, err := p.docSet(ctx, d, who, year)
	if err != nil {
		return out, err
	}
	tag, found := set.Tags[strings.ToLower(p.mapping.QueueTag)]
	if out.TagFound = found; !found {
		return out, nil
	}
	expenses, docs, err := p.unlinked(ctx, d, who, year)
	if err != nil {
		return out, err
	}
	tagged := slices.DeleteFunc(slices.Clone(docs), func(doc sources.ReceiptDoc) bool { return !slices.Contains(doc.Tags, tag) })
	out.Docs, out.Expenses = len(tagged), len(expenses)
	out.Matches = p.matcher().Reverse(tagged, expenses, docs)
	return out, nil
}

// LinkedRow is one expense with its scans.
type LinkedRow struct {
	Expense sources.ReceiptExpense
	Docs    []LinkedDoc
}

// LinkedDoc is one linked scan; Doc is nil if gone.
type LinkedDoc struct {
	ID  int64
	Doc *sources.ReceiptDoc
}

// LinkedView lists a year's linked expenses, latest change first.
type LinkedView struct {
	Links
	Year         int
	Query        string
	Rows         []LinkedRow
	Total, Shown int
}

// Linked lists the year's linked expenses matching query (number,
// vendor, note, invoice number, amount), latest change first.
func Linked(ctx context.Context, d *sql.DB, who *access.Principal, year int, query string) (LinkedView, error) {
	p, err := openPair(d, who)
	if err != nil {
		return LinkedView{}, err
	}
	out := LinkedView{Links: p.links(), Year: year, Query: strings.TrimSpace(query)}
	if !p.mapping.Complete() {
		return out, ErrMapping
	}
	set, err := p.expenses(ctx, d, who)
	if err != nil {
		return out, err
	}
	linked := slices.DeleteFunc(inYear(set.Expenses, year), func(e sources.ReceiptExpense) bool {
		return len(docIDs(e.Custom[p.mapping.LinkSlot-1])) == 0 || !matchesQuery(e, out.Query)
	})
	slices.SortStableFunc(linked, func(a, b sources.ReceiptExpense) int { return b.Updated.Compare(a.Updated) })
	out.Total = len(linked)
	linked = linked[:min(len(linked), linkedShown)]
	out.Shown = len(linked)

	var ids []int64
	for _, e := range linked {
		row := LinkedRow{Expense: e}
		for _, id := range docIDs(e.Custom[p.mapping.LinkSlot-1]) {
			row.Docs = append(row.Docs, LinkedDoc{ID: id})
			ids = append(ids, id)
		}
		out.Rows = append(out.Rows, row)
	}
	docs, err := p.docsByID(ctx, d, who, ids)
	if err != nil {
		return out, err
	}
	for _, row := range out.Rows {
		for i := range row.Docs {
			row.Docs[i].Doc = docs[row.Docs[i].ID]
		}
	}
	return out, nil
}

// matchesQuery tells whether an expense's number, vendor, note, custom
// values or amount ("89,70" or "89.70") contain the query.
func matchesQuery(e sources.ReceiptExpense, query string) bool {
	if query == "" {
		return true
	}
	amount := strconv.FormatFloat(e.Amount, 'f', 2, 64)
	hay := strings.ToLower(strings.Join(append([]string{e.Number, e.Vendor, e.Notes, amount, strings.ReplaceAll(amount, ".", ",")}, e.Custom[:]...), " "))
	return strings.Contains(hay, strings.ToLower(query))
}

func (p pair) docsByID(ctx context.Context, d *sql.DB, who *access.Principal, ids []int64) (map[int64]*sources.ReceiptDoc, error) {
	out := map[int64]*sources.ReceiptDoc{}
	if len(ids) == 0 {
		return out, nil
	}
	sctx, err := p.sourceCtx(d, who, p.docs)
	if err != nil {
		return nil, err
	}
	docs, err := sources.PaperlessDocsByID(ctx, sctx, ids)
	if err != nil {
		return nil, FetchError{err.Error()}
	}
	for i := range docs {
		out[docs[i].ID] = &docs[i]
	}
	return out, nil
}

// IgnoredView lists what the user hid, resolved where still there.
type IgnoredView struct {
	Links
	Expenses []IgnoredExpense
	Docs     []IgnoredDoc
}

// IgnoredExpense is a hidden expense; Expense is nil if gone.
type IgnoredExpense struct {
	Key     string
	Reason  Reason
	Expense *sources.ReceiptExpense
}

// IgnoredDoc is a hidden scan; Doc is nil if gone.
type IgnoredDoc struct {
	ID     int64
	Reason Reason
	Doc    *sources.ReceiptDoc
}

// Ignored lists the hidden expenses and scans of the picked pair.
func Ignored(ctx context.Context, d *sql.DB, who *access.Principal) (IgnoredView, error) {
	p, err := openPair(d, who)
	if err != nil {
		return IgnoredView{}, err
	}
	out := IgnoredView{Links: p.links()}
	if keys := p.state.ignored(KindExpense, p.ninja.ID); len(keys) > 0 {
		set, err := p.expenses(ctx, d, who)
		if err != nil {
			return out, err
		}
		for _, key := range keys {
			row := IgnoredExpense{Key: key, Reason: p.state.reason(KindExpense, p.ninja.ID, key)}
			for i := range set.Expenses {
				if set.Expenses[i].Key == key {
					row.Expense = &set.Expenses[i]
				}
			}
			out.Expenses = append(out.Expenses, row)
		}
	}
	var ids []int64
	for _, raw := range p.state.ignored(KindDoc, p.docs.ID) {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	docs, err := p.docsByID(ctx, d, who, ids)
	if err != nil {
		return out, err
	}
	for _, id := range ids {
		out.Docs = append(out.Docs, IgnoredDoc{ID: id, Reason: p.state.reason(KindDoc, p.docs.ID, strconv.FormatInt(id, 10)), Doc: docs[id]})
	}
	return out, nil
}

// Ignore hides an expense or a scan, with an optional reason, or shows
// it again.
func Ignore(d *sql.DB, who *access.Principal, kind Kind, id string, hide Hide, why Reason) error {
	p, err := openPair(d, who)
	if err != nil {
		return err
	}
	conn := p.ninja.ID
	if kind == KindDoc {
		conn = p.docs.ID
	}
	if id = strings.TrimSpace(id); id == "" || (kind != KindExpense && kind != KindDoc) {
		return ErrNotFound
	}
	if !slices.Contains(Reasons, why) {
		why = ReasonNone
	}
	return saveState(d, who, func(s *state) { s.setIgnored(kind, conn, id, hide, why) })
}
