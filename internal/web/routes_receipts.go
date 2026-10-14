package web

// The receipts page links Invoice Ninja expenses to Paperless scans:
//
//	GET  /receipts               shell: Verbund pick (?scope=…, kept), tabs, year
//	GET  /receipts/part          one tab's content (htmx, can be slow)
//	GET  /receipts/search        manual scan search for one expense (htmx)
//	GET  /receipts/combos        1∶n combos of one expense, on request (htmx)
//	GET  /receipts/expenses      manual expense search for one scan (htmx)
//	GET  /receipts/new-expense   a new expense, filled from a scan (htmx)
//	GET  /receipts/thumb/{id}    a scan's thumbnail, through Andon (CSP)
//	POST /receipts/review        every link or new expense, spelled out to confirm
//	POST /receipts/apply         writes the confirmed picks
//	POST /receipts/link          undo of an unlink, written at once
//	POST /receipts/unlink|ignore|fields

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
	"andon/internal/services/workbench"
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
	mux.HandleFunc("POST /receipts/review", d.authed(d.handleReceiptReview))
	mux.HandleFunc("POST /receipts/apply", d.authed(d.handleReceiptApply))
	mux.HandleFunc("GET /receipts/thumb/{id}", d.authed(d.handleReceiptThumb))
	mux.HandleFunc("POST /receipts/link", d.authed(d.handleReceiptLink))
	mux.HandleFunc("POST /receipts/unlink", d.authed(d.handleReceiptUnlink))
	mux.HandleFunc("POST /receipts/ignore", d.authed(d.handleReceiptIgnore))
	mux.HandleFunc("POST /receipts/fields", d.authed(d.handleReceiptFields))
}

func (d Deps) handleReceipts(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := receiptsQueryOf(r)

	// A Verbund picked (bench_scopes) is kept for the next visit.
	if scope := r.URL.Query().Get(benchScope); scope != "" {
		if err := receipts.Choose(d.DB, ctx.Who, scope); err != nil {
			http.Redirect(w, r, q.back("error", errKey(err)), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/receipts?"+q.Values().Encode(), http.StatusSeeOther)
		return
	}
	setup, err := receipts.SetupOf(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	query := r.URL.Query()

	// A success only for at least one step; "0" is no news.
	hunted, _ := strconv.Atoi(query.Get("hunted"))
	failed, _ := strconv.Atoi(query.Get("failed"))
	_ = d.Page(w, ctx, "receipts", http.StatusOK, map[string]any{
		"Setup": setup, "Q": q, "Tabs": receiptTabs, "Years": years(),
		"Done": query.Get("done"), "Error": query.Get("error"), "Note": query.Get("note"), "Hunted": hunted, "Failed": failed, "Who": query.Get("who"), "Reason": query.Get("reason"), "Other": query.Get("other"),
		"Undo": receiptUndo{Expense: query.Get("undo_expense"), Doc: query.Get("undo_doc")},
	})
}

// receiptUndo is an unlink the page offers to take back.
type receiptUndo struct{ Expense, Doc string }

// handleReceiptsPart renders one tab; errors show inside it.
func (d Deps) handleReceiptsPart(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	d.receiptsPart(w, r, ctx, receiptsQueryOf(r), nil)
}

// receiptsPart renders the tab of q; extra adds to its values (a refused
// field mapping: what was chosen and why it was refused).
func (d Deps) receiptsPart(w http.ResponseWriter, r *http.Request, ctx Ctx, q receiptsQuery, extra func(values map[string]any)) {
	var err error
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
	values["Counts"], _ = receipts.CountsOf(r.Context(), d.DB, ctx.Who, q.Tab, q.Year)
	values["Tabs"], values["Years"] = receiptTabs, years()
	if extra != nil {
		extra(values)
	}
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
	id := formID(r, "doc")
	view, err := receipts.FindExpenses(r.Context(), d.DB, ctx.Who, id, q.Year, r.FormValue("q"))
	values := map[string]any{"Q": q, "S": view}
	if err != nil {
		values["Error"] = receiptError(err)
	}
	_ = d.Page(w, ctx, "receipts_expenses", http.StatusOK, values)
}

// handleReceiptDraft shows the form for a new expense from a scan.
func (d Deps) handleReceiptDraft(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id := formID(r, "doc")
	view, err := receipts.Draft(r.Context(), d.DB, ctx.Who, id)
	values := map[string]any{"Q": receiptsQueryOf(r), "S": view}
	if err != nil {
		values["Error"] = receiptError(err)
	}
	_ = d.Page(w, ctx, "receipts_draft", http.StatusOK, values)
}

// huntSteps reads the match tab's picks: case=Kx9 per expense, in page
// order, and its choice: choice_Kx9=link:12,13 | mail:5:4711 | "".
func huntSteps(form url.Values) []workbench.Step {
	var out []workbench.Step
	for _, k := range form["case"] {
		act, rest, _ := strings.Cut(form.Get("choice_"+k), ":")
		switch act {
		case receipts.ActLink:
			out = append(out, workbench.Step{Case: k, Action: act, Fields: map[string]string{receipts.FieldDocs: rest}})
		case receipts.ActMail:
			out = append(out, workbench.Step{Case: k, Action: act, Fields: map[string]string{receipts.FieldMail: rest}})
		}
	}
	return out
}

// reviewSteps reads whatever asks for a review: the match tab's picks
// (huntSteps), one link from a search (expense=Kx9, docs=11,12 or one
// doc=… per ticked box), or a new expense from a scan (doc, amount …).
func reviewSteps(form url.Values) []workbench.Step {
	if len(form["case"]) > 0 {
		return huntSteps(form)
	}
	if e := form.Get("expense"); e != "" {
		docs := slices.DeleteFunc(append(strings.Split(form.Get("docs"), ","), form["doc"]...), func(s string) bool { return strings.TrimSpace(s) == "" })
		return []workbench.Step{{Case: e, Action: receipts.ActLink, Fields: map[string]string{receipts.FieldDocs: strings.Join(docs, ",")}}}
	}
	id, err := strconv.ParseInt(form.Get("doc"), 10, 64)
	if err != nil {
		return nil
	}
	fields := map[string]string{}
	for _, f := range []string{receipts.FieldAmount, receipts.FieldVendor, receipts.FieldNotes} {
		fields[f] = strings.TrimSpace(form.Get(f))
	}
	fields[receipts.FieldDay] = dateOrEmpty(form.Get(receipts.FieldDay))
	return []workbench.Step{{Case: receipts.ScanCase(id), Action: receipts.ActCreate, Fields: fields}}
}

// handleReceiptReview shows what the picks would write; nothing is
// written yet.
func (d Deps) handleReceiptReview(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := receiptsQueryOf(r)
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	plan, err := receipts.Review(r.Context(), d.DB, ctx.Who, q.Year, reviewSteps(r.PostForm))
	if err != nil {
		http.Redirect(w, r, q.back("error", receiptError(err)), http.StatusSeeOther)
		return
	}
	_ = d.Page(w, ctx, "receipts_review", http.StatusOK, map[string]any{"Plan": plan, "Q": q})
}

// handleReceiptApply writes the steps the review showed and the user
// confirmed; each is checked again against fresh data.
func (d Deps) handleReceiptApply(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := receiptsQueryOf(r)
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	res, err := receipts.Apply(r.Context(), d.DB, ctx.Who, q.Year, benchSteps(r.PostForm, receipts.HuntFields), d.clientIP(r))
	if err != nil {
		http.Redirect(w, r, q.back("error", receiptError(err)), http.StatusSeeOther)
		return
	}
	var more []string
	if f := res.Failed; len(f) > 0 {
		more = []string{"failed", strconv.Itoa(len(f)), "who", f[0].Name, "reason", f[0].Reason}
	}
	http.Redirect(w, r, q.back("hunted", strconv.Itoa(res.Done), more...), http.StatusSeeOther)
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
	number, err := receipts.Link(r.Context(), d.DB, ctx.Who, r.FormValue("expense"), ids, d.clientIP(r))
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
	id := formID(r, "doc")
	expense := r.FormValue("expense")
	if err := receipts.Unlink(r.Context(), d.DB, ctx.Who, expense, id, d.clientIP(r)); err != nil {
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
		n := formID(r, name)
		return n
	}
	m := receipts.Mapping{InvoiceSlot: int(num("invoice_slot")), LinkSlot: int(num("link_slot")), FieldInvoice: num("field_invoice"),
		FieldExpense: num("field_expense"), FieldLink: num("field_link"), FieldAmount: num("field_amount"), QueueTag: r.FormValue("queue_tag")}
	err := receipts.SaveFields(d.DB, ctx.Who, m)
	soft := r.Header.Get("HX-Request") != ""
	switch {
	case err != nil && soft:
		// The tab again, with the error and the fields as chosen.
		q.Tab = tabFields
		d.receiptsPart(w, r, ctx, q, func(values map[string]any) {
			if setup, ok := values["V"].(receipts.FieldSetup); ok {
				setup.Mapping, setup.Suggested = m, false
				values["V"] = setup
			}
			values["FieldsError"] = errKey(err)
		})
	case err != nil:
		http.Redirect(w, r, q.back("error", errKey(err), "tab", tabFields), http.StatusSeeOther)
	case soft:
		w.Header().Set("HX-Redirect", q.back("note", "receipts.fields_saved"))
	default:
		http.Redirect(w, r, q.back("note", "receipts.fields_saved"), http.StatusSeeOther)
	}
}
