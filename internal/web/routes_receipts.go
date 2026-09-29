package web

// The receipts page links Invoice Ninja expenses to Paperless scans:
//
//	GET  /receipts               shell: connection pick, tabs, year
//	GET  /receipts/part          one tab's content (htmx, can be slow)
//	GET  /receipts/search        manual scan search for one expense (htmx)
//	GET  /receipts/combos        1∶n combos of one expense, on request (htmx)
//	GET  /receipts/expenses      manual expense search for one scan (htmx)
//	GET  /receipts/new-expense   a new expense, filled from a scan (htmx)
//	GET  /receipts/thumb/{id}    a scan's thumbnail, through Andon (CSP)
//	POST /receipts/pick          which Invoice Ninja and Paperless to use
//	POST /receipts/link|link-many|unlink|ignore|fields|new-expense

import (
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/services/receipts"
)

// Tabs of the receipts page.
const (
	tabMatch   = "match"
	tabQueue   = "queue"
	tabLinked  = "linked"
	tabIgnored = "ignored"
	tabFields  = "fields"
)

var receiptTabs = []string{tabMatch, tabQueue, tabLinked, tabIgnored, tabFields}

const yearsBack = 6

// receiptsQuery is the page state carried in links and forms; Find
// filters the linked tab.
type receiptsQuery struct {
	Tab  string
	Year int
	Find string
}

func receiptsQueryOf(r *http.Request) receiptsQuery {
	q := receiptsQuery{Tab: r.FormValue("tab"), Find: strings.TrimSpace(r.FormValue("find"))}
	if !slices.Contains(receiptTabs, q.Tab) {
		q.Tab = tabMatch
	}
	q.Year, _ = strconv.Atoi(r.FormValue("year"))
	if now := time.Now().Year(); q.Year > now || q.Year < now-yearsBack {
		q.Year = now
	}
	return q
}

// Values is the state as query parameters.
func (q receiptsQuery) Values() url.Values {
	v := url.Values{"tab": {q.Tab}, "year": {strconv.Itoa(q.Year)}}
	if q.Find != "" {
		v.Set("find", q.Find)
	}
	return v
}

// Link is the page with one part changed: ("tab", "queue").
func (q receiptsQuery) Link(key, value string) string {
	v := q.Values()
	v.Set(key, value)
	if key == "tab" {
		v.Del("find")
	}
	return "/receipts?" + v.Encode()
}

// Part is the URL of the tab's content.
func (q receiptsQuery) Part() string { return "/receipts/part?" + q.Values().Encode() }

// Back is where a form returns to, with a message and its params:
// ("note", "receipts.unlinked", "expense", "Kx9").
func (q receiptsQuery) back(key, value string, more ...string) string {
	v := q.Values()
	v.Set(key, value)
	for i := 0; i+1 < len(more); i += 2 {
		v.Set(more[i], more[i+1])
	}
	return "/receipts?" + v.Encode()
}

func years() []int {
	now := time.Now().Year()
	out := make([]int, 0, yearsBack)
	for y := now; y > now-yearsBack; y-- {
		out = append(out, y)
	}
	return out
}

func (d Deps) registerReceiptRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /receipts", d.authed(d.handleReceipts))
	mux.HandleFunc("GET /receipts/part", d.authed(d.handleReceiptsPart))
	mux.HandleFunc("GET /receipts/search", d.authed(d.handleReceiptsSearch))
	mux.HandleFunc("GET /receipts/combos", d.authed(d.handleReceiptCombos))
	mux.HandleFunc("GET /receipts/expenses", d.authed(d.handleReceiptExpenses))
	mux.HandleFunc("GET /receipts/new-expense", d.authed(d.handleReceiptDraft))
	mux.HandleFunc("POST /receipts/new-expense", d.authed(d.handleReceiptCreate))
	mux.HandleFunc("POST /receipts/link-many", d.authed(d.handleReceiptLinkMany))
	mux.HandleFunc("GET /receipts/thumb/{id}", d.authed(d.handleReceiptThumb))
	mux.HandleFunc("POST /receipts/pick", d.authed(d.handleReceiptsPick))
	mux.HandleFunc("POST /receipts/link", d.authed(d.handleReceiptLink))
	mux.HandleFunc("POST /receipts/unlink", d.authed(d.handleReceiptUnlink))
	mux.HandleFunc("POST /receipts/ignore", d.authed(d.handleReceiptIgnore))
	mux.HandleFunc("POST /receipts/fields", d.authed(d.handleReceiptFields))
}

func (d Deps) handleReceipts(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	setup, err := receipts.SetupOf(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	query := r.URL.Query()
	_ = d.Page(w, ctx, "receipts", http.StatusOK, map[string]any{
		"Setup": setup, "Q": receiptsQueryOf(r), "Tabs": receiptTabs, "Years": years(),
		"Done": query.Get("done"), "Error": query.Get("error"), "Note": query.Get("note"), "Linked": query.Get("linked"), "Other": query.Get("other"),
		"Undo": receiptUndo{Expense: query.Get("undo_expense"), Doc: query.Get("undo_doc")},
	})
}

// receiptUndo is an unlink the page offers to take back.
type receiptUndo struct{ Expense, Doc string }

// handleReceiptsPart renders one tab; errors show inside it.
func (d Deps) handleReceiptsPart(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	var err error
	q := receiptsQueryOf(r)
	values := map[string]any{"Q": q}
	switch q.Tab {
	case tabQueue:
		values["V"], err = receipts.Queue(r.Context(), d.DB, ctx.Who, q.Year)
	case tabLinked:
		values["V"], err = receipts.Linked(r.Context(), d.DB, ctx.Who, q.Year, q.Find)
	case tabIgnored:
		values["V"], err = receipts.Ignored(r.Context(), d.DB, ctx.Who)
	case tabFields:
		values["V"], err = receipts.Fields(r.Context(), d.DB, ctx.Who)
	default:
		values["V"], err = receipts.Suggest(r.Context(), d.DB, ctx.Who, q.Year)
	}
	if err != nil {
		values["Error"], values["Mapping"] = receiptError(err), errors.Is(err, receipts.ErrMapping)
	}
	// The tab and year chips get their numbers with the part.
	values["Counts"], _ = receipts.CountsOf(r.Context(), d.DB, ctx.Who, q.Year)
	values["Tabs"], values["Years"] = receiptTabs, years()
	_ = d.Page(w, ctx, "receipts_part", http.StatusOK, values)
}

// receiptError is an error's i18n key, or a service's own message.
func receiptError(err error) string {
	var fetch receipts.FetchError
	if errors.As(err, &fetch) {
		return fetch.Msg
	}
	return errKey(err)
}

func (d Deps) handleReceiptsSearch(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := receiptsQueryOf(r)
	in := receipts.SearchInput{Query: r.FormValue("q"), Correspondent: r.FormValue("correspondent"), From: dateOrEmpty(r.FormValue("from")),
		To: dateOrEmpty(r.FormValue("to")), Preset: receipts.Preset(r.FormValue("preset")), UnlinkedOnly: r.FormValue("unlinked") != "", Year: q.Year}
	in.With, _ = strconv.ParseInt(r.FormValue("with"), 10, 64)
	if in.Preset != "" {
		// A preset sets the window itself; dates typed for another would win.
		in.From, in.To = "", ""
	}
	view, err := receipts.Search(r.Context(), d.DB, ctx.Who, r.FormValue("expense"), in)
	values := map[string]any{"Q": q, "S": view, "Presets": receipts.Presets, "Slot": searchSlot(r)}
	if err != nil {
		values["Error"] = receiptError(err)
	}
	_ = d.Page(w, ctx, "receipts_search", http.StatusOK, values)
}

// searchSlot is the element a search panel lives in: the expense's own
// slot, or a scan's panel in "receipts first" ("panel-204").
func searchSlot(r *http.Request) string {
	if slot := r.FormValue("slot"); slotPattern.MatchString(slot) {
		return slot
	}
	return "search-" + r.FormValue("expense")
}

var slotPattern = regexp.MustCompile(`^(search|panel)-[A-Za-z0-9_-]+$`)

// handleReceiptCombos looks for 1∶n combos of one expense on request.
func (d Deps) handleReceiptCombos(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := receiptsQueryOf(r)
	view, err := receipts.FindCombos(r.Context(), d.DB, ctx.Who, r.FormValue("expense"), q.Year)
	values := map[string]any{"Q": q, "S": view}
	if err != nil {
		values["Error"] = receiptError(err)
	}
	_ = d.Page(w, ctx, "receipts_combos", http.StatusOK, values)
}

// handleReceiptExpenses searches the expenses one scan could belong to.
func (d Deps) handleReceiptExpenses(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := receiptsQueryOf(r)
	id, _ := strconv.ParseInt(r.FormValue("doc"), 10, 64)
	view, err := receipts.FindExpenses(r.Context(), d.DB, ctx.Who, id, q.Year, r.FormValue("q"))
	values := map[string]any{"Q": q, "S": view}
	if err != nil {
		values["Error"] = receiptError(err)
	}
	_ = d.Page(w, ctx, "receipts_expenses", http.StatusOK, values)
}

// handleReceiptDraft shows the form for a new expense from a scan.
func (d Deps) handleReceiptDraft(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, _ := strconv.ParseInt(r.FormValue("doc"), 10, 64)
	view, err := receipts.Draft(r.Context(), d.DB, ctx.Who, id)
	values := map[string]any{"Q": receiptsQueryOf(r), "S": view}
	if err != nil {
		values["Error"] = receiptError(err)
	}
	_ = d.Page(w, ctx, "receipts_draft", http.StatusOK, values)
}

func (d Deps) handleReceiptCreate(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := receiptsQueryOf(r)
	id, _ := strconv.ParseInt(r.FormValue("doc"), 10, 64)
	amount, _ := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(r.FormValue("amount")), ",", "."), 64)
	in := receipts.NewExpense{Amount: amount, Day: dateOrEmpty(r.FormValue("day")), Vendor: r.FormValue("vendor"), Notes: r.FormValue("notes")}
	number, err := receipts.Create(r.Context(), d.DB, ctx.Who, id, in, ClientIP(r))
	if err != nil {
		http.Redirect(w, r, q.back("error", receiptError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, q.back("done", number), http.StatusSeeOther)
}

// handleReceiptLinkMany links the sure matches ticked, "Kx9:201" each.
func (d Deps) handleReceiptLinkMany(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := receiptsQueryOf(r)
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	var pairs []receipts.Pair
	for _, v := range r.PostForm["pair"] {
		key, raw, ok := strings.Cut(v, ":")
		id, err := strconv.ParseInt(raw, 10, 64)
		if ok && err == nil && key != "" {
			pairs = append(pairs, receipts.Pair{Expense: key, Docs: []int64{id}})
		}
	}
	done, err := receipts.LinkMany(r.Context(), d.DB, ctx.Who, pairs, ClientIP(r))
	if err != nil {
		http.Redirect(w, r, q.back("error", receiptError(err), "linked", strconv.Itoa(done)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, q.back("linked", strconv.Itoa(done)), http.StatusSeeOther)
}

// dateOrEmpty keeps a well-formed "2026-08-01" only.
func dateOrEmpty(v string) string {
	if _, err := time.Parse(time.DateOnly, v); err != nil {
		return ""
	}
	return v
}

// thumbCache lets the browser keep thumbnails for a while.
const thumbCache = "private, max-age=300"

func (d Deps) handleReceiptThumb(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	body, kind, err := receipts.Thumb(r.Context(), d.DB, ctx.Who, id)
	if err != nil || !strings.HasPrefix(kind, "image/") {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Cache-Control", thumbCache)
	w.Write(body)
}

func (d Deps) handleReceiptsPick(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	ninja, _ := strconv.ParseInt(r.FormValue("ninja"), 10, 64)
	paperless, _ := strconv.ParseInt(r.FormValue("paperless"), 10, 64)
	q := receiptsQueryOf(r)
	if err := receipts.Choose(d.DB, ctx.Who, ninja, paperless); err != nil {
		http.Redirect(w, r, q.back("error", errKey(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/receipts?"+q.Values().Encode(), http.StatusSeeOther)
}

func (d Deps) handleReceiptLink(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := receiptsQueryOf(r)
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	// "docs" is a list ("11,12"), "doc" one per ticked box.
	var ids []int64
	for _, part := range append(strings.Split(r.FormValue("docs"), ","), r.PostForm["doc"]...) {
		if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		http.Redirect(w, r, q.back("error", errKey(receipts.ErrNotFound)), http.StatusSeeOther)
		return
	}
	number, err := receipts.Link(r.Context(), d.DB, ctx.Who, r.FormValue("expense"), ids, ClientIP(r))
	var elsewhere receipts.LinkedElsewhere
	if errors.As(err, &elsewhere) {
		http.Redirect(w, r, q.back("error", elsewhere.Error(), "other", elsewhere.Number), http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Redirect(w, r, q.back("error", receiptError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, q.back("done", number), http.StatusSeeOther)
}

func (d Deps) handleReceiptUnlink(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := receiptsQueryOf(r)
	id, _ := strconv.ParseInt(r.FormValue("doc"), 10, 64)
	expense := r.FormValue("expense")
	if err := receipts.Unlink(r.Context(), d.DB, ctx.Who, expense, id, ClientIP(r)); err != nil {
		http.Redirect(w, r, q.back("error", receiptError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, q.back("note", "receipts.unlinked", "undo_expense", expense, "undo_doc", strconv.FormatInt(id, 10)), http.StatusSeeOther)
}

func (d Deps) handleReceiptIgnore(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := receiptsQueryOf(r)
	hide, note := receipts.Hidden, "receipts.ignored_note"
	if r.FormValue("show") != "" {
		hide, note = receipts.Shown, "receipts.shown_note"
	}
	if err := receipts.Ignore(d.DB, ctx.Who, receipts.Kind(r.FormValue("kind")), r.FormValue("id"), hide, receipts.Reason(r.FormValue("reason"))); err != nil {
		http.Redirect(w, r, q.back("error", errKey(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, q.back("note", note), http.StatusSeeOther)
}

func (d Deps) handleReceiptFields(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := receiptsQueryOf(r)
	num := func(name string) int64 {
		n, _ := strconv.ParseInt(r.FormValue(name), 10, 64)
		return n
	}
	m := receipts.Mapping{InvoiceSlot: int(num("invoice_slot")), LinkSlot: int(num("link_slot")), FieldInvoice: num("field_invoice"),
		FieldExpense: num("field_expense"), FieldLink: num("field_link"), FieldAmount: num("field_amount"), QueueTag: r.FormValue("queue_tag")}
	if err := receipts.SaveFields(d.DB, ctx.Who, m); err != nil {
		http.Redirect(w, r, q.back("error", errKey(err), "tab", tabFields), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, q.back("note", "receipts.fields_saved"), http.StatusSeeOther)
}
