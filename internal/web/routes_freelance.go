package web

import (
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/services/access"
	"andon/internal/services/assist"
	"andon/internal/services/billing"
	"andon/internal/services/mailfwd"

	"andon/internal/services/svcdata"
	"andon/internal/services/timer"
	"andon/internal/widgets"
)

// kimaiBack is the view a Kimai Lite form returns to.
type kimaiBack string

const (
	backTile kimaiBack = ""
	backDay  kimaiBack = "day"
)

// handleKimaiTimer runs a Kimai Lite action and answers with the
// refreshed tile, or today's list when the action came from there. A
// rejected form comes back with its error.
func (d Deps) handleKimaiTimer(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	num := func(name string) int64 {
		n := formID(r, name)
		return n
	}
	req := timer.Request{Action: timer.Action(r.FormValue("action")), Project: num("project"), Activity: num("activity"),
		Sheet: num("sheet"), Note: strings.TrimSpace(r.FormValue("note")),
		StartNote: strings.TrimSpace(r.FormValue("start_note")), Begin: r.FormValue("begin"), End: r.FormValue("end"),
		Tags: r.FormValue("tags"), Billable: enums.Billable(r.FormValue("billable")), At: r.FormValue("at")}
	back := kimaiBack(r.FormValue("back"))
	err = timer.Run(r.Context(), d.DB, ctx.Who, id, req, d.clientIP(r))
	if errors.Is(err, timer.ErrNotTimer) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	// Form errors go back to the form they came from.
	if key := formErrKey(err); key != "" {
		switch req.Action {
		case timer.ActionCreate, timer.ActionEdit:
			d.renderKimaiForm(w, r, ctx, id, req, back, key)
			return
		case timer.ActionSplit, timer.ActionDelete:
			d.renderKimaiDay(w, r, ctx, id, key)
			return
		}
	}
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	if back == backDay {
		d.renderKimaiDay(w, r, ctx, id, "")
		return
	}
	d.renderFragment(w, r, ctx, id, svcdata.Force)
}

// formErrKey is the message key of an error a form can show; "" for
// others.
func formErrKey(err error) string {
	for _, known := range []error{timer.ErrBadRange, timer.ErrBadSplit, timer.ErrRejected} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return ""
}

// newEntryStep rounds the add-entry form's default times (5 minutes).
const newEntryStep = 5 * time.Minute

// formTimeLayout is what a datetime-local input sends and shows.
const formTimeLayout = "2006-01-02T15:04"

// handleKimaiNew swaps a Kimai Lite tile for its add-entry form: the
// last hour, ending now.
func (d Deps) handleKimaiNew(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	end := time.Now().Truncate(newEntryStep)
	req := timer.Request{Action: timer.ActionCreate, Begin: end.Add(-time.Hour).Format(formTimeLayout), End: end.Format(formTimeLayout)}
	d.renderKimaiForm(w, r, ctx, id, req, backTile, "")
}

// handleKimaiEdit swaps a Kimai Lite tile for the edit form of the
// running entry or one of today's (?sheet=77&back=day).
func (d Deps) handleKimaiEdit(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	sheet, _ := strconv.ParseInt(r.URL.Query().Get("sheet"), 10, 64)
	req, err := timer.Draft(r.Context(), d.DB, ctx.Who, id, sheet)
	if errors.Is(err, timer.ErrNotTimer) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	d.renderKimaiForm(w, r, ctx, id, req, kimaiBack(r.URL.Query().Get("back")), "")
}

func (d Deps) renderKimaiForm(w http.ResponseWriter, r *http.Request, ctx Ctx, id int64, req timer.Request, back kimaiBack, errKey string) {
	catalog, err := timer.Catalog(r.Context(), d.DB, ctx.Who, id)
	if errors.Is(err, timer.ErrNotTimer) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	_ = d.Page(w, ctx, "kimai_new", http.StatusOK, map[string]any{"PlacementID": id, "Catalog": catalog, "Req": req, "Error": errKey,
		"Back": string(back), "Split": splitOf(req), "ThemeURL": ""})
}

// splitTimes is the edit form's split field: default, first, last.
type splitTimes struct{ At, Min, Max string }

// splitOf offers the middle of a stopped entry, inside its first and
// last minute: 09:00–12:00 → 10:30 (09:01–11:59).
func splitOf(req timer.Request) splitTimes {
	begin, errB := time.Parse(formTimeLayout, req.Begin)
	end, errE := time.Parse(formTimeLayout, req.End)
	if errB != nil || errE != nil || !end.After(begin) {
		return splitTimes{}
	}
	mid := begin.Add(end.Sub(begin) / 2).Truncate(time.Minute)
	return splitTimes{mid.Format(formTimeLayout), begin.Add(time.Minute).Format(formTimeLayout), end.Add(-time.Minute).Format(formTimeLayout)}
}

// handleKimaiDay swaps a Kimai Lite tile for today's entries.
func (d Deps) handleKimaiDay(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d.renderKimaiDay(w, r, ctx, id, "")
}

func (d Deps) renderKimaiDay(w http.ResponseWriter, r *http.Request, ctx Ctx, id int64, errKey string) {
	day, err := timer.Day(r.Context(), d.DB, ctx.Who, id)
	if errors.Is(err, timer.ErrNotTimer) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	_ = d.Page(w, ctx, "kimai_day", http.StatusOK, map[string]any{"PlacementID": id, "Rows": widgets.DayRows(day, time.Now()),
		"Error": errKey, "ThemeURL": ""})
}

// handleKimaiPin pins a project-activity pair on the tile, or unpins it.
func (d Deps) handleKimaiPin(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	project, _ := strconv.ParseInt(r.FormValue("project"), 10, 64)
	activity, _ := strconv.ParseInt(r.FormValue("activity"), 10, 64)
	err = timer.Pin(r.Context(), d.DB, ctx.Who, id, project, activity)
	if errors.Is(err, timer.ErrNotTimer) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	d.renderFragment(w, r, ctx, id, svcdata.Cached)
}

// RegisterBillingRoutes wires invoice drafts and the tax year package.
func (d Deps) RegisterBillingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /billing", d.authed(d.handleBillingPage))
	mux.HandleFunc("POST /billing/draft", d.authed(d.handleBillingDraft))
	mux.HandleFunc("GET /billing/export", d.authed(d.handleBillingExport))
	mux.HandleFunc("POST /billing/mail", d.authed(d.handleMailForward))
	mux.HandleFunc("POST /billing/mail/read", d.authed(d.handleMailRead))
	mux.HandleFunc("POST /billing/payment", d.authed(d.handlePaymentBook))
	d.registerReceiptRoutes(mux)
}

// handlePaymentBook books a matched bank income in Invoice Ninja.
func (d Deps) handlePaymentBook(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	space := formID(r, "space_id")
	invoice := formID(r, "invoice_id")
	if err := billing.Book(r.Context(), d.DB, ctx.Who, space, r.FormValue("txn"), invoice, d.clientIP(r)); err != nil {
		d.billingPage(w, r, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	http.Redirect(w, r, "/billing?booked="+url.QueryEscape(r.FormValue("number"))+"#payments", http.StatusSeeOther)
}

func (d Deps) billingPage(w http.ResponseWriter, r *http.Request, ctx Ctx, status int, extra map[string]any) {
	drafts, err := billing.Candidates(r.Context(), d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	mails, sent, err := mailfwd.List(r.Context(), d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	payments, err := billing.Payments(r.Context(), d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	// Sure matches first; the rest wants a second look.
	sort.SliceStable(payments, func(i, j int) bool { return payments[i].Sure() && !payments[j].Sure() })
	year := time.Now().Year()
	values := map[string]any{"Drafts": drafts, "Mails": mails, "Payments": payments, "Summary": billingSummary(drafts, payments, len(mails), sent), "Booked": r.URL.Query().Get("booked"), "Assist": assist.Enabled(), "Spaces": access.EditableSpaces(ctx.Who), "Years": []int{year, year - 1}}
	for k, v := range extra {
		values[k] = v
	}
	_ = d.Page(w, ctx, "billing", status, values)
}

func (d Deps) handleBillingPage(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	query := r.URL.Query()
	d.billingPage(w, r, ctx, http.StatusOK, map[string]any{"Created": query.Get("created"), "Forwarded": query.Get("forwarded")})
}

func (d Deps) handleBillingDraft(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	space := formID(r, "space_id")
	customer := formID(r, "customer_id")
	mode := billing.KeepSheets
	if r.FormValue("mark_exported") != "" {
		mode = billing.MarkSheets
	}
	number, err := billing.Create(r.Context(), d.DB, ctx.Who, space, customer, mode, d.clientIP(r))
	if err != nil {
		d.billingPage(w, r, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	http.Redirect(w, r, "/billing?created="+url.QueryEscape(number), http.StatusSeeOther)
}

func (d Deps) handleBillingExport(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	space := formID(r, "space_id")
	year := formInt(r, "year")
	name, blob, err := billing.Export(r.Context(), d.DB, ctx.Who, space, year, d.clientIP(r))
	if err != nil {
		d.billingPage(w, r, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Write(blob)
}

// handleMailForward sends one invoice mail's attachments to Paperless.
func (d Deps) handleMailForward(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	conn := formID(r, "conn")
	uid, _ := strconv.ParseUint(r.FormValue("uid"), 10, 32)
	n, err := mailfwd.Forward(r.Context(), d.DB, ctx.Who, conn, uint32(uid), d.clientIP(r))
	if err != nil {
		d.billingPage(w, r, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	http.Redirect(w, r, "/billing?forwarded="+strconv.Itoa(n), http.StatusSeeOther)
}

// handleMailRead lets Claude read one invoice mail's attachments.
func (d Deps) handleMailRead(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	conn := formID(r, "conn")
	uid, _ := strconv.ParseUint(r.FormValue("uid"), 10, 32)
	if _, err := mailfwd.Read(r.Context(), d.DB, ctx.Who, conn, uint32(uid), d.clientIP(r)); err != nil {
		d.billingPage(w, r, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	http.Redirect(w, r, "/billing#mail-"+strconv.FormatUint(uid, 10), http.StatusSeeOther)
}

// billingSum is the line above the billing page: what is waiting.
type billingSum struct {
	Unbilled             float64
	Customers, Payments  int
	PaymentsToCheck      int
	Mails, MailsInLedger int
}

func billingSummary(drafts []billing.Candidate, payments []billing.Payment, mails, sent int) billingSum {
	sum := billingSum{Customers: len(drafts), Payments: len(payments), Mails: mails, MailsInLedger: sent}
	for _, d := range drafts {
		sum.Unbilled += d.Total
	}
	for _, p := range payments {
		if !p.Sure() {
			sum.PaymentsToCheck++
		}
	}
	return sum
}
