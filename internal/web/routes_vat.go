package web

// The VAT check works through Invoice Ninja's bank feed, one VAT period
// at a time; nothing reaches Invoice Ninja before the user confirmed:
//
//	GET  /vat          the period's open transactions, proposals, check
//	POST /vat/review   the ticked steps, spelled out, to confirm
//	POST /vat/apply    writes the confirmed steps
//	POST /vat/skip     shows a transaction set aside again
//	POST /vat/guess    asks the language model about the open ones

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"andon/internal/metrics"
	"andon/internal/services/assist"
	"andon/internal/services/vatcheck"
)

func (d Deps) registerVATRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /vat", d.authed(d.handleVAT))
	mux.HandleFunc("POST /vat/review", d.authed(d.handleVATReview))
	mux.HandleFunc("POST /vat/apply", d.authed(d.handleVATApply))
	mux.HandleFunc("POST /vat/skip", d.authed(d.handleVATSkip))
	mux.HandleFunc("POST /vat/guess", d.authed(d.handleVATGuess))
}

// vatQuery is the connection and period carried in links and forms.
type vatQuery struct {
	Conn int64
	Span string
}

func vatQueryOf(r *http.Request) vatQuery {
	return vatQuery{Conn: formID(r, "conn"), Span: r.FormValue("span")}
}

// back is the page with a note: ("done", "3", "skipped", "1").
func (q vatQuery) back(pairs ...string) string {
	v := url.Values{}
	if q.Conn != 0 {
		v.Set("conn", strconv.FormatInt(q.Conn, 10))
	}
	if q.Span != "" {
		v.Set("span", q.Span)
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		v.Set(pairs[i], pairs[i+1])
	}
	return "/vat?" + v.Encode()
}

func (d Deps) handleVAT(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := vatQueryOf(r)
	page, err := vatcheck.Open(r.Context(), d.DB, ctx.Who, q.Conn, q.Span, time.Now().UTC())
	query := r.URL.Query()
	values := map[string]any{"P": page, "Note": query.Get("note"), "Error": query.Get("error"), "Reason": query.Get("reason")}
	for _, k := range []string{"done", "skipped", "failed", "guessed"} {
		values[strings.ToUpper(k[:1])+k[1:]], _ = strconv.Atoi(query.Get(k))
	}
	if err != nil {
		values["Error"] = vatError(err)
	}
	_ = d.Page(w, ctx, "vat", http.StatusOK, values)
}

// vatError is an error's i18n key, or a service's own message.
func vatError(err error) string {
	var fetch vatcheck.FetchError
	if errors.As(err, &fetch) {
		return fetch.Msg
	}
	return errKey(err)
}

// vatSteps reads the ticked rows; each row's fields carry its key:
// pick=Kx9, act_Kx9=create, vendor_Kx9=…, cat_Kx9=…, tax_Kx9=19|none.
func vatSteps(r *http.Request) []vatcheck.Step {
	if err := r.ParseForm(); err != nil {
		return nil
	}
	var out []vatcheck.Step
	for _, k := range r.PostForm["pick"] {
		s := vatcheck.Step{Txn: k, Action: metrics.BankAction(r.PostForm.Get("act_" + k)), Expense: r.PostForm.Get("exp_" + k),
			Invoice: r.PostForm.Get("inv_" + k), Vendor: r.PostForm.Get("vendor_" + k), Category: r.PostForm.Get("cat_" + k)}
		if rate, err := strconv.ParseFloat(r.PostForm.Get("tax_"+k), 64); err == nil {
			s.TaxRate, s.Taxed = rate, true
		}
		if s.Action != metrics.ActionNone {
			out = append(out, s)
		}
	}
	return out
}

// handleVATReview shows what would be written; nothing is written yet.
func (d Deps) handleVATReview(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := vatQueryOf(r)
	plan, err := vatcheck.Review(r.Context(), d.DB, ctx.Who, q.Conn, vatSteps(r))
	if err != nil {
		http.Redirect(w, r, q.back("error", vatError(err)), http.StatusSeeOther)
		return
	}
	_ = d.Page(w, ctx, "vat_review", http.StatusOK, map[string]any{"Plan": plan, "Q": q})
}

// handleVATApply writes the steps the review showed and the user
// confirmed; each is checked again against fresh data.
func (d Deps) handleVATApply(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := vatQueryOf(r)
	res, err := vatcheck.Apply(r.Context(), d.DB, ctx.Who, q.Conn, vatSteps(r), d.clientIP(r))
	if err != nil {
		http.Redirect(w, r, q.back("error", vatError(err)), http.StatusSeeOther)
		return
	}
	pairs := []string{"done", strconv.Itoa(res.Done), "skipped", strconv.Itoa(res.Skipped)}
	if len(res.Failed) > 0 {
		first := res.Failed[0]
		pairs = append(pairs, "failed", strconv.Itoa(len(res.Failed)), "reason", first.Payer+": "+first.Reason)
	}
	http.Redirect(w, r, q.back(pairs...), http.StatusSeeOther)
}

func (d Deps) handleVATSkip(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := vatQueryOf(r)
	if err := vatcheck.Skip(d.DB, ctx.Who, q.Conn, r.FormValue("txn"), vatcheck.Shown); err != nil {
		http.Redirect(w, r, q.back("error", vatError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, q.back("note", "vatcheck.shown"), http.StatusSeeOther)
}

func (d Deps) handleVATGuess(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := vatQueryOf(r)
	n, err := vatcheck.Guess(r.Context(), d.DB, ctx.Who, q.Conn, q.Span, time.Now().UTC())
	if err != nil {
		key := vatError(err)
		if errors.Is(err, assist.ErrUnreadable) {
			key = "vatcheck.guess_failed"
		}
		http.Redirect(w, r, q.back("error", key), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, q.back("guessed", strconv.Itoa(n)), http.StatusSeeOther)
}
