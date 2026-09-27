package web

// The receipts page links Invoice Ninja expenses to Paperless scans:
//
//	GET  /receipts               shell: connection pick, tabs, year
//	GET  /receipts/part          one tab's content (htmx, can be slow)
//	GET  /receipts/search        manual scan search for one expense (htmx)
//	GET  /receipts/thumb/{id}    a scan's thumbnail, through Andon (CSP)
//	POST /receipts/pick          which Invoice Ninja and Paperless to use
//	POST /receipts/link|unlink|ignore|fields

import (
	"errors"
	"net/http"
	"net/url"
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

// receiptsQuery is the page state carried in links and forms.
type receiptsQuery struct {
	Tab   string
	Year  int
	Combo bool
}

func receiptsQueryOf(r *http.Request) receiptsQuery {
	q := receiptsQuery{Tab: r.FormValue("tab"), Combo: r.FormValue("combo") != ""}
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
	if q.Combo {
		v.Set("combo", "1")
	}
	return v
}

// Link is the page with one part changed: ("tab", "queue").
func (q receiptsQuery) Link(key, value string) string {
	v := q.Values()
	v.Set(key, value)
	if key == "combo" && value == "" {
		v.Del("combo")
	}
	return "/receipts?" + v.Encode()
}

// Part is the URL of the tab's content.
func (q receiptsQuery) Part() string { return "/receipts/part?" + q.Values().Encode() }

// Back is where a form returns to, with a message.
func (q receiptsQuery) back(key, value string) string {
	v := q.Values()
	v.Set(key, value)
	return "/receipts?" + v.Encode()
}

func (q receiptsQuery) mode() receipts.ComboMode {
	if q.Combo {
		return receipts.WithCombos
	}
	return receipts.Singles
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
	mux.HandleFunc("GET /receipts", d.handleReceipts)
	mux.HandleFunc("GET /receipts/part", d.handleReceiptsPart)
	mux.HandleFunc("GET /receipts/search", d.handleReceiptsSearch)
	mux.HandleFunc("GET /receipts/thumb/{id}", d.handleReceiptThumb)
	mux.HandleFunc("POST /receipts/pick", d.handleReceiptsPick)
	mux.HandleFunc("POST /receipts/link", d.handleReceiptLink)
	mux.HandleFunc("POST /receipts/unlink", d.handleReceiptUnlink)
	mux.HandleFunc("POST /receipts/ignore", d.handleReceiptIgnore)
	mux.HandleFunc("POST /receipts/fields", d.handleReceiptFields)
}

func (d Deps) handleReceipts(w http.ResponseWriter, r *http.Request) {
	ctx, err := d.Require(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	setup, err := receipts.SetupOf(d.DB, ctx.Who)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	query := r.URL.Query()
	_ = d.Page(w, ctx, "receipts", http.StatusOK, map[string]any{
		"Setup": setup, "Q": receiptsQueryOf(r), "Tabs": receiptTabs, "Years": years(),
		"Done": query.Get("done"), "Error": query.Get("error"), "Note": query.Get("note"),
	})
}

// handleReceiptsPart renders one tab; errors show inside it.
func (d Deps) handleReceiptsPart(w http.ResponseWriter, r *http.Request) {
	ctx, err := d.Require(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	q := receiptsQueryOf(r)
	values := map[string]any{"Q": q}
	switch q.Tab {
	case tabQueue:
		values["V"], err = receipts.Queue(r.Context(), d.DB, ctx.Who, q.Year)
	case tabLinked:
		values["V"], err = receipts.Linked(r.Context(), d.DB, ctx.Who)
	case tabIgnored:
		values["V"], err = receipts.Ignored(r.Context(), d.DB, ctx.Who)
	case tabFields:
		values["V"], err = receipts.Fields(r.Context(), d.DB, ctx.Who)
	default:
		values["V"], err = receipts.Suggest(r.Context(), d.DB, ctx.Who, q.Year, q.mode())
	}
	if err != nil {
		values["Error"], values["Mapping"] = receiptError(err), errors.Is(err, receipts.ErrMapping)
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

func (d Deps) handleReceiptsSearch(w http.ResponseWriter, r *http.Request) {
	ctx, err := d.Require(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	q := receiptsQueryOf(r)
	in := receipts.SearchInput{Query: r.FormValue("q"), Correspondent: r.FormValue("correspondent"), From: dateOrEmpty(r.FormValue("from")),
		To: dateOrEmpty(r.FormValue("to")), Preset: receipts.Preset(r.FormValue("preset")), UnlinkedOnly: r.FormValue("unlinked") != "", Year: q.Year}
	view, err := receipts.Search(r.Context(), d.DB, ctx.Who, r.FormValue("expense"), in)
	values := map[string]any{"Q": q, "S": view, "Presets": receipts.Presets}
	if err != nil {
		values["Error"] = receiptError(err)
	}
	_ = d.Page(w, ctx, "receipts_search", http.StatusOK, values)
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

func (d Deps) handleReceiptThumb(w http.ResponseWriter, r *http.Request) {
	ctx, err := d.Require(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
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

func (d Deps) handleReceiptsPick(w http.ResponseWriter, r *http.Request) {
	ctx, err := d.Require(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	ninja, _ := strconv.ParseInt(r.FormValue("ninja"), 10, 64)
	paperless, _ := strconv.ParseInt(r.FormValue("paperless"), 10, 64)
	q := receiptsQueryOf(r)
	if err := receipts.Choose(d.DB, ctx.Who, ninja, paperless); err != nil {
		http.Redirect(w, r, q.back("error", errKey(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/receipts?"+q.Values().Encode(), http.StatusSeeOther)
}

func (d Deps) handleReceiptLink(w http.ResponseWriter, r *http.Request) {
	ctx, err := d.Require(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	q := receiptsQueryOf(r)
	var ids []int64
	for _, part := range strings.Split(r.FormValue("docs"), ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		http.Redirect(w, r, q.back("error", errKey(receipts.ErrNotFound)), http.StatusSeeOther)
		return
	}
	number, err := receipts.Link(r.Context(), d.DB, ctx.Who, r.FormValue("expense"), ids, ClientIP(r))
	if err != nil {
		http.Redirect(w, r, q.back("error", receiptError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, q.back("done", number), http.StatusSeeOther)
}

func (d Deps) handleReceiptUnlink(w http.ResponseWriter, r *http.Request) {
	ctx, err := d.Require(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	q := receiptsQueryOf(r)
	id, _ := strconv.ParseInt(r.FormValue("doc"), 10, 64)
	if err := receipts.Unlink(r.Context(), d.DB, ctx.Who, r.FormValue("expense"), id, ClientIP(r)); err != nil {
		http.Redirect(w, r, q.back("error", receiptError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, q.back("note", "receipts.unlinked"), http.StatusSeeOther)
}

func (d Deps) handleReceiptIgnore(w http.ResponseWriter, r *http.Request) {
	ctx, err := d.Require(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	q := receiptsQueryOf(r)
	hide, note := receipts.Hidden, "receipts.ignored_note"
	if r.FormValue("show") != "" {
		hide, note = receipts.Shown, "receipts.shown_note"
	}
	if err := receipts.Ignore(d.DB, ctx.Who, receipts.Kind(r.FormValue("kind")), r.FormValue("id"), hide); err != nil {
		http.Redirect(w, r, q.back("error", errKey(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, q.back("note", note), http.StatusSeeOther)
}

func (d Deps) handleReceiptFields(w http.ResponseWriter, r *http.Request) {
	ctx, err := d.Require(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	q := receiptsQueryOf(r)
	num := func(name string) int64 {
		n, _ := strconv.ParseInt(r.FormValue(name), 10, 64)
		return n
	}
	m := receipts.Mapping{InvoiceSlot: int(num("invoice_slot")), LinkSlot: int(num("link_slot")), FieldInvoice: num("field_invoice"),
		FieldExpense: num("field_expense"), FieldLink: num("field_link"), FieldAmount: num("field_amount"), QueueTag: r.FormValue("queue_tag")}
	if err := receipts.SaveFields(d.DB, ctx.Who, m); err != nil {
		http.Redirect(w, r, q.back("error", errKey(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, q.back("note", "receipts.fields_saved"), http.StatusSeeOther)
}
