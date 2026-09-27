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
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/outbound"
	"andon/internal/services/access"
	auditsvc "andon/internal/services/audit"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
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
)

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
	linkedShown   = 40
	urlSeparator  = " "
)

// ── connections ──

// Choice is one connection the user may use here.
type Choice struct {
	ID          int64
	Name, Space string
	NeedsToken  bool // personal credentials, the user has not stored his
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
	s, err := loadState(d, who)
	if err != nil {
		return Setup{}, err
	}
	out.Ninja, out.Paperless = pickOf(out.Ninjas, s.Ninja), pickOf(out.Docs, s.Paperless)
	savedPick := out.Ninja != 0 && out.Paperless != 0
	if !savedPick {
		out.Ninja, out.Paperless = only(out.Ninjas), only(out.Docs)
		out.Pick = len(out.Ninjas) > 0 && len(out.Docs) > 0 && (out.Ninja == 0 || out.Paperless == 0)
	}
	return out, nil
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
	res, err := svcdata.Get(ctx, d, expenseSource, nil, p.ninja, &uid, svcdata.Cached)
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
	res, err := svcdata.Get(ctx, d, docSource, map[string]any{"year": year}, p.docs, &uid, svcdata.Cached)
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
	return svcdata.SourceCtx(d, conn, who.UserID)
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
	Mode           ComboMode
	Matches        []ExpenseMatch
	Expenses, Docs int
}

// Suggest lists the year's unlinked expenses with their best scans.
func Suggest(ctx context.Context, d *sql.DB, who *access.Principal, year int, mode ComboMode) (Suggestions, error) {
	p, err := openPair(d, who)
	if err != nil {
		return Suggestions{}, err
	}
	out := Suggestions{Links: p.links(), Mapping: p.mapping, Year: year, Mode: mode}
	if !p.mapping.Complete() {
		return out, ErrMapping
	}
	expenses, docs, err := p.unlinked(ctx, d, who, year)
	if err != nil {
		return out, err
	}
	out.Expenses, out.Docs = len(expenses), len(docs)
	out.Matches = p.matcher().Matches(expenses, docs, mode)
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
	_ = saveState(d, who, func(s *state) {
		for _, group := range learned {
			for _, name := range group[1:] {
				s.Aliases.learn(group[0], name)
			}
		}
		s.Backfilled = append(s.Backfilled, key)
	})
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
	docs = slices.DeleteFunc(docs, func(doc sources.ReceiptDoc) bool { return !slices.Contains(doc.Tags, tag) })
	out.Docs, out.Expenses = len(docs), len(expenses)
	out.Matches = p.matcher().Reverse(docs, expenses)
	return out, nil
}

// LinkedRow is one expense with one of its scans (nil if gone).
type LinkedRow struct {
	Expense sources.ReceiptExpense
	DocID   int64
	Doc     *sources.ReceiptDoc
}

// LinkedView lists the latest links.
type LinkedView struct {
	Links
	Rows []LinkedRow
}

// Linked lists the most recently changed linked expenses with their scans.
func Linked(ctx context.Context, d *sql.DB, who *access.Principal) (LinkedView, error) {
	p, err := openPair(d, who)
	if err != nil {
		return LinkedView{}, err
	}
	out := LinkedView{Links: p.links()}
	if !p.mapping.Complete() {
		return out, ErrMapping
	}
	set, err := p.expenses(ctx, d, who)
	if err != nil {
		return out, err
	}
	linked := slices.DeleteFunc(slices.Clone(set.Expenses), func(e sources.ReceiptExpense) bool {
		return len(docIDs(e.Custom[p.mapping.LinkSlot-1])) == 0
	})
	slices.SortStableFunc(linked, func(a, b sources.ReceiptExpense) int { return b.Updated.Compare(a.Updated) })
	linked = linked[:min(len(linked), linkedShown)]

	var ids []int64
	for _, e := range linked {
		for _, id := range docIDs(e.Custom[p.mapping.LinkSlot-1]) {
			out.Rows = append(out.Rows, LinkedRow{Expense: e, DocID: id})
			ids = append(ids, id)
		}
	}
	docs, err := p.docsByID(ctx, d, who, ids)
	if err != nil {
		return out, err
	}
	for i := range out.Rows {
		out.Rows[i].Doc = docs[out.Rows[i].DocID]
	}
	return out, nil
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
	Expense *sources.ReceiptExpense
}

// IgnoredDoc is a hidden scan; Doc is nil if gone.
type IgnoredDoc struct {
	ID  int64
	Doc *sources.ReceiptDoc
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
			row := IgnoredExpense{Key: key}
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
		out.Docs = append(out.Docs, IgnoredDoc{ID: id, Doc: docs[id]})
	}
	return out, nil
}

// Ignore hides an expense or a scan, or shows it again.
func Ignore(d *sql.DB, who *access.Principal, kind Kind, id string, hide Hide) error {
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
	return saveState(d, who, func(s *state) { s.setIgnored(kind, conn, id, hide) })
}

// ── writing ──

// Link ties an expense to one scan or, for a 1∶n combo, several.
func Link(ctx context.Context, d *sql.DB, who *access.Principal, expenseKey string, ids []int64, ip string) (string, error) {
	p, err := openPair(d, who)
	if err != nil {
		return "", err
	}
	w, err := p.writer(ctx, d, who, expenseKey, ids)
	if err != nil {
		return "", err
	}
	if err := w.link(ctx); err != nil {
		return "", err
	}

	number := w.number()
	learned := p.state.Aliases
	for _, doc := range w.docs {
		learned.learn(w.expense.Vendor, doc.Correspondent)
	}
	err = saveState(d, who, func(s *state) {
		s.Aliases = learned
		s.setIgnored(KindExpense, p.ninja.ID, expenseKey, Shown)
		for _, id := range ids {
			s.setIgnored(KindDoc, p.docs.ID, strconv.FormatInt(id, 10), Shown)
		}
	})
	svcdata.Forget(p.ninja.ID)
	svcdata.Forget(p.docs.ID)
	if err != nil {
		return number, err
	}
	return number, auditsvc.Log(d, &who.UserID, "receipts.link", number, ip, map[string]any{"docs": idList(ids)})
}

// Unlink removes one scan from an expense; other scans stay linked.
func Unlink(ctx context.Context, d *sql.DB, who *access.Principal, expenseKey string, id int64, ip string) error {
	p, err := openPair(d, who)
	if err != nil {
		return err
	}
	w, err := p.writer(ctx, d, who, expenseKey, nil)
	if err != nil {
		return err
	}
	if err := w.unlink(ctx, id); err != nil {
		return err
	}
	svcdata.Forget(p.ninja.ID)
	svcdata.Forget(p.docs.ID)
	return auditsvc.Log(d, &who.UserID, "receipts.unlink", w.number(), ip, map[string]any{"docs": idList([]int64{id})})
}

func idList(ids []int64) string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = strconv.FormatInt(id, 10)
	}
	return strings.Join(out, ",")
}

// writer holds everything one link or unlink needs, read fresh.
type writer struct {
	Links
	mapping       Mapping
	ninja, docsTo outbound.Target
	expense       sources.ReceiptExpense
	docs          []sources.ReceiptDoc
}

func (p pair) writer(ctx context.Context, d *sql.DB, who *access.Principal, expenseKey string, ids []int64) (writer, error) {
	if !p.mapping.Complete() {
		return writer{}, ErrMapping
	}
	if sources.IsDemo(p.ninja.URL) || sources.IsDemo(p.docs.URL) {
		return writer{}, ErrDemo
	}
	w := writer{Links: p.links(), mapping: p.mapping}
	nctx, err := p.sourceCtx(d, who, p.ninja)
	if err != nil {
		return writer{}, err
	}
	dctx, err := p.sourceCtx(d, who, p.docs)
	if err != nil {
		return writer{}, err
	}
	w.ninja = outbound.Target{URL: p.ninja.URL, Token: nctx.Secret, VerifyTLS: p.ninja.VerifyTLS}
	w.docsTo = outbound.Target{URL: p.docs.URL, Token: dctx.Secret, VerifyTLS: p.docs.VerifyTLS}

	if w.expense, err = sources.NinjaExpenseByKey(ctx, nctx, expenseKey); err != nil {
		return writer{}, FetchError{err.Error()}
	}
	if w.expense.Key == "" {
		return writer{}, ErrNotFound
	}
	if len(ids) == 0 {
		return w, nil
	}
	found, err := sources.PaperlessDocsByID(ctx, dctx, ids)
	if err != nil {
		return writer{}, FetchError{err.Error()}
	}
	for _, id := range ids {
		i := slices.IndexFunc(found, func(doc sources.ReceiptDoc) bool { return doc.ID == id })
		if i < 0 {
			return writer{}, ErrNotFound
		}
		w.docs = append(w.docs, found[i])
	}
	return w, nil
}

// number is what the scans get as expense number: its number, else its id.
func (w writer) number() string {
	if w.expense.Number != "" {
		return w.expense.Number
	}
	return w.expense.Key
}

func slotName(slot int) string { return "custom_value" + strconv.Itoa(slot) }

func (w writer) link(ctx context.Context) error {
	m := w.mapping
	urls := make([]string, len(w.docs))
	for i, doc := range w.docs {
		urls[i] = w.DocURL(doc.ID)
	}
	onExpense := map[string]string{slotName(m.LinkSlot): strings.Join(urls, urlSeparator)}

	// One scan: the invoice number travels to whichever side lacks it.
	invoice := ""
	if len(w.docs) == 1 {
		invoice = w.expense.Custom[m.InvoiceSlot-1]
		if invoice == "" {
			invoice, _ = w.docs[0].Custom[m.FieldInvoice].(string)
			invoice = strings.TrimSpace(invoice)
		}
		if invoice != "" {
			onExpense[slotName(m.InvoiceSlot)] = invoice
		}
	}
	if err := outbound.NinjaExpenseSet(ctx, w.ninja, w.expense.Key, onExpense); err != nil {
		return FetchError{err.Error()}
	}

	var done []int64
	for _, doc := range w.docs {
		onDoc := map[int64]any{m.FieldExpense: w.number(), m.FieldLink: w.ExpenseURL(w.expense.Key)}
		if invoice != "" {
			onDoc[m.FieldInvoice] = invoice
		}
		if err := outbound.PaperlessFieldsSet(ctx, w.docsTo, doc.ID, onDoc); err != nil {
			w.rollback(ctx, done)
			return FetchError{err.Error()}
		}
		done = append(done, doc.ID)
	}
	return nil
}

// rollback undoes a half-written link, best effort: the expense's link
// value, then the scans written so far.
func (w writer) rollback(ctx context.Context, done []int64) {
	m := w.mapping
	_ = outbound.NinjaExpenseSet(ctx, w.ninja, w.expense.Key, map[string]string{slotName(m.LinkSlot): w.expense.Custom[m.LinkSlot-1]})
	for _, id := range done {
		_ = outbound.PaperlessFieldsSet(ctx, w.docsTo, id, map[int64]any{m.FieldExpense: "", m.FieldLink: ""})
	}
}

func (w writer) unlink(ctx context.Context, id int64) error {
	m := w.mapping
	var keep []string
	for _, other := range docIDs(w.expense.Custom[m.LinkSlot-1]) {
		if other != id {
			keep = append(keep, w.DocURL(other))
		}
	}
	if err := outbound.NinjaExpenseSet(ctx, w.ninja, w.expense.Key, map[string]string{slotName(m.LinkSlot): strings.Join(keep, urlSeparator)}); err != nil {
		return FetchError{err.Error()}
	}
	if err := outbound.PaperlessFieldsSet(ctx, w.docsTo, id, map[int64]any{m.FieldExpense: "", m.FieldLink: ""}); err != nil {
		return FetchError{err.Error()}
	}
	return nil
}

// ── search ──

// Preset is a ready-made search for one expense.
type Preset string

const (
	PresetDate     Preset = "around_date"
	PresetAmount   Preset = "amount"
	PresetVendor   Preset = "vendor"
	PresetInvoice  Preset = "invoice"
	PresetYear     Preset = "year"
	PresetUnlinked Preset = "unlinked"
)

// Presets in the order the page offers them.
var Presets = []Preset{PresetDate, PresetAmount, PresetVendor, PresetInvoice, PresetYear, PresetUnlinked}

const searchAround = 14 // days either side of the expense

// SearchInput is the search form.
type SearchInput struct {
	Query, Correspondent, From, To string
	Preset                         Preset
	UnlinkedOnly                   bool
	Year                           int
}

// SearchView is the search panel of one expense.
type SearchView struct {
	Links
	Expense sources.ReceiptExpense
	Input   SearchInput
	Hits    []sources.ReceiptDoc
}

// Search looks for scans of one expense by hand.
func Search(ctx context.Context, d *sql.DB, who *access.Principal, expenseKey string, in SearchInput) (SearchView, error) {
	p, err := openPair(d, who)
	if err != nil {
		return SearchView{}, err
	}
	out := SearchView{Links: p.links(), Input: in}
	set, err := p.expenses(ctx, d, who)
	if err != nil {
		return out, err
	}
	i := slices.IndexFunc(set.Expenses, func(e sources.ReceiptExpense) bool { return e.Key == expenseKey })
	if i < 0 {
		return out, ErrNotFound
	}
	out.Expense = set.Expenses[i]

	s := searchOf(out.Expense, in, p.mapping)
	if (in.UnlinkedOnly || in.Preset == PresetUnlinked) && p.mapping.FieldExpense > 0 {
		docs, err := p.docSet(ctx, d, who, in.Year)
		if err != nil {
			return out, err
		}
		for _, f := range docs.Fields {
			if f.ID == p.mapping.FieldExpense {
				s.EmptyField = f.Name
			}
		}
	}
	sctx, err := p.sourceCtx(d, who, p.docs)
	if err != nil {
		return out, err
	}
	if out.Hits, err = sources.PaperlessSearch(ctx, sctx, s); err != nil {
		return out, FetchError{err.Error()}
	}
	return out, nil
}

// searchOf turns the form and preset into a Paperless search: the year
// by default, the preset narrowing it, typed fields winning.
func searchOf(e sources.ReceiptExpense, in SearchInput, m Mapping) sources.DocSearch {
	s := sources.DocSearch{From: strconv.Itoa(in.Year) + "-01-01", To: strconv.Itoa(in.Year) + "-12-31", Correspondent: strings.TrimSpace(in.Correspondent)}
	switch in.Preset {
	case PresetDate:
		if day, err := time.Parse(time.DateOnly, e.Day); err == nil {
			s.From, s.To = day.AddDate(0, 0, -searchAround).Format(time.DateOnly), day.AddDate(0, 0, searchAround).Format(time.DateOnly)
		}
	case PresetAmount:
		s.Query = strconv.FormatFloat(e.Amount, 'f', 2, 64)
		s.TitleContent = strings.ReplaceAll(s.Query, ".", ",")
	case PresetVendor:
		s.Correspondent, s.TitleContent = e.Vendor, e.Vendor
	case PresetInvoice:
		if m.InvoiceSlot > 0 {
			s.Query = e.Custom[m.InvoiceSlot-1]
		}
		if s.Query == "" {
			s.Query = e.Number
		}
	}
	if in.From != "" {
		s.From = in.From
	}
	if in.To != "" {
		s.To = in.To
	}
	if q := strings.TrimSpace(in.Query); q != "" {
		s.Query = q
	}
	return s
}

// Thumb reads a scan's thumbnail from the picked Paperless.
func Thumb(ctx context.Context, d *sql.DB, who *access.Principal, id int64) ([]byte, string, error) {
	p, err := openPair(d, who)
	if err != nil {
		return nil, "", err
	}
	sctx, err := p.sourceCtx(d, who, p.docs)
	if err != nil {
		return nil, "", err
	}
	return sources.PaperlessThumb(ctx, sctx, id)
}
