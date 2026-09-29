package receipts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/outbound"
	"andon/internal/services/access"
	auditsvc "andon/internal/services/audit"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
)

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
		s.setIgnored(KindExpense, p.ninja.ID, expenseKey, Shown, ReasonNone)
		for _, id := range ids {
			s.setIgnored(KindDoc, p.docs.ID, strconv.FormatInt(id, 10), Shown, ReasonNone)
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
	if err := connections.Writable(d, who, p.ninja.ID, p.docs.ID); err != nil {
		return writer{}, err
	}
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
	if err := checkFree(p.mapping, w.expense, w.docs); err != nil {
		return writer{}, err
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
	// Scans already linked stay; the new ones join them.
	var urls []string
	had := docIDs(w.expense.Custom[m.LinkSlot-1])
	for _, id := range had {
		urls = append(urls, w.DocURL(id))
	}
	for _, doc := range w.docs {
		if !slices.Contains(had, doc.ID) {
			urls = append(urls, w.DocURL(doc.ID))
		}
	}
	onExpense := map[string]string{slotName(m.LinkSlot): strings.Join(urls, urlSeparator)}

	// One scan: the invoice number travels to whichever side lacks it.
	invoice := ""
	if len(urls) == 1 {
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
			if undoErr := w.rollback(ctx, done); undoErr != nil {
				return FetchError{err.Error() + "; " + undoErr.Error()}
			}
			return FetchError{err.Error()}
		}
		done = append(done, doc.ID)
	}
	return nil
}

// rollback undoes a half-written link, best effort: the expense's link
// value, then the scans written so far. What cannot be undone is logged
// with its ids and returned, so the user learns what is left half-linked.
func (w writer) rollback(ctx context.Context, done []int64) error {
	m := w.mapping
	var failed []error
	if err := outbound.NinjaExpenseSet(ctx, w.ninja, w.expense.Key, map[string]string{slotName(m.LinkSlot): w.expense.Custom[m.LinkSlot-1]}); err != nil {
		slog.Error("receipts: undo expense link", "expense", w.expense.Key, "err", err)
		failed = append(failed, fmt.Errorf("expense %s still linked", w.expense.Key))
	}
	for _, id := range done {
		if err := outbound.PaperlessFieldsSet(ctx, w.docsTo, id, map[int64]any{m.FieldExpense: "", m.FieldLink: ""}); err != nil {
			slog.Error("receipts: undo document link", "document", id, "err", err)
			failed = append(failed, fmt.Errorf("document %d still linked", id))
		}
	}
	return errors.Join(failed...)
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
	PresetDate    Preset = "around_date"
	PresetAmount  Preset = "amount"
	PresetVendor  Preset = "vendor"
	PresetInvoice Preset = "invoice"
	PresetYear    Preset = "year"
)

// Presets in the order the page offers them; "only without expense" is
// the form's checkbox.
var Presets = []Preset{PresetDate, PresetAmount, PresetVendor, PresetInvoice, PresetYear}

const searchAround = 14 // days either side of the expense

// SearchInput is the search form; With is a scan already picked (from
// "receipts first"), kept among the hits and ticked.
type SearchInput struct {
	Query, Correspondent, From, To string
	Preset                         Preset
	UnlinkedOnly                   bool
	Year                           int
	With                           int64
}

// SearchView is the search panel of one expense; From and To is the
// window searched.
type SearchView struct {
	Links
	Expense  sources.ReceiptExpense
	Input    SearchInput
	From, To string
	Hits     []SearchHit
}

// SearchHit is a scan found by hand, scored against the expense;
// LinkedTo names the expense it already belongs to, Own is the scan's
// own amount for a combo put together by hand (0 = unknown).
type SearchHit struct {
	Candidate
	LinkedTo string
	Own      float64
	Picked   bool
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
	out.From, out.To = s.From, s.To
	if in.UnlinkedOnly && p.mapping.FieldExpense > 0 {
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
	found, err := sources.PaperlessSearch(ctx, sctx, s)
	if err != nil {
		return out, FetchError{err.Error()}
	}
	if in.With > 0 && !slices.ContainsFunc(found, func(doc sources.ReceiptDoc) bool { return doc.ID == in.With }) {
		if doc, err := p.doc(ctx, d, who, in.With); err == nil {
			found = append(found, doc)
		}
	}
	x := p.matcher()
	for _, doc := range found {
		hit := SearchHit{Candidate: x.score(out.Expense, doc), LinkedTo: linkedTo(p.mapping, doc), Picked: doc.ID == in.With}
		if in.UnlinkedOnly && hit.LinkedTo != "" {
			continue
		}
		if own, ok := x.comboAmount(doc); ok {
			hit.Own = own
		} else {
			hit.Own = hit.Amount
		}
		out.Hits = append(out.Hits, hit)
	}
	// The scan brought along first, then by score.
	slices.SortStableFunc(out.Hits, func(a, b SearchHit) int {
		switch {
		case a.Picked && !b.Picked:
			return -1
		case b.Picked && !a.Picked:
			return 1
		}
		return b.Score - a.Score
	})
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
