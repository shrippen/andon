package web

// The VAT check works through Invoice Ninja's bank feed, one VAT period
// at a time; nothing reaches Invoice Ninja before the user confirmed:
//
//	GET  /vat          the period's open transactions, proposals, check
//	POST /vat/review   the ticked steps, spelled out, to confirm
//	POST /vat/apply    writes the confirmed steps
//	POST /vat/restore  brings a transaction archived as private back
//	POST /vat/accounts names each bank account business or private
//	POST /vat/guess    asks the language model about the open ones
//	POST /vat/filed    marks the period's figures as filed

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"andon/internal/services/assist"
	"andon/internal/services/vatcheck"
	"andon/internal/services/verbund"
	"andon/internal/services/workbench"
)

func (d Deps) registerVATRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /vat", d.authed(d.handleVAT))
	mux.HandleFunc("POST /vat/review", d.authed(d.handleVATReview))
	mux.HandleFunc("POST /vat/apply", d.authed(d.handleVATApply))
	mux.HandleFunc("POST /vat/restore", d.authed(d.handleVATRestore))
	mux.HandleFunc("POST /vat/accounts", d.authed(d.handleVATAccounts))
	mux.HandleFunc("POST /vat/guess", d.authed(d.handleVATGuess))
	mux.HandleFunc("POST /vat/filed", d.authed(d.handleVATFiled))
}

// vatQuery is the Verbund and period carried in links and forms.
type vatQuery struct {
	Scope string
	Span  string
}

func vatQueryOf(r *http.Request) vatQuery {
	return vatQuery{Scope: r.FormValue(benchScope), Span: r.FormValue("span")}
}

// back is the page with a note: ("done", "3", "skipped", "1").
func (q vatQuery) back(pairs ...string) string {
	v := url.Values{}
	if q.Scope != "" {
		v.Set(benchScope, q.Scope)
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
	page, err := vatcheck.Open(r.Context(), d.DB, ctx.Who, q.Scope, q.Span, time.Now().UTC())
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

// vatSteps reads the ticked rows (benchSteps); all_private archives
// every apart=… row instead.
func vatSteps(r *http.Request) []workbench.Step {
	if err := r.ParseForm(); err != nil {
		return nil
	}

	// "Archive all as private": the private accounts' rest, nothing else.
	if r.PostForm.Get("all_private") == "" {
		return benchSteps(r.PostForm, vatcheck.Fields)
	}
	var out []workbench.Step
	for _, k := range r.PostForm["apart"] {
		out = append(out, workbench.Step{Case: k, Action: workbench.ActionSkip})
	}
	return out
}

// handleVATReview shows what would be written; nothing is written yet.
func (d Deps) handleVATReview(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := vatQueryOf(r)
	plan, err := vatcheck.Review(r.Context(), d.DB, ctx.Who, q.Scope, vatSteps(r))
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
	res, err := vatcheck.Apply(r.Context(), d.DB, ctx.Who, q.Scope, vatSteps(r), d.clientIP(r))
	if err != nil {
		http.Redirect(w, r, q.back("error", vatError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, q.back(benchNote(res)...), http.StatusSeeOther)
}

func (d Deps) handleVATRestore(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := vatQueryOf(r)
	if err := vatcheck.Restore(r.Context(), d.DB, ctx.Who, q.Scope, r.FormValue("txn"), d.clientIP(r)); err != nil {
		http.Redirect(w, r, q.back("error", vatError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, q.back("note", "vatcheck.shown"), http.StatusSeeOther)
}

// rolePrefix starts each account's field: role:<connection>:<account key>.
const rolePrefix = "role:"

// handleVATAccounts stores each account's role.
func (d Deps) handleVATAccounts(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := vatQueryOf(r)
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	var roles []vatcheck.Role
	for name, v := range r.PostForm {
		conn, key, ok := strings.Cut(strings.TrimPrefix(name, rolePrefix), ":")
		id, err := strconv.ParseInt(conn, 10, 64)
		if !strings.HasPrefix(name, rolePrefix) || !ok || err != nil {
			continue
		}
		roles = append(roles, vatcheck.Role{ConnID: id, Key: key, Role: verbund.Role(v[0])})
	}
	if err := vatcheck.SaveRoles(r.Context(), d.DB, ctx.Who, q.Scope, roles, d.clientIP(r)); err != nil {
		http.Redirect(w, r, q.back("error", vatError(err))+"#accounts", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, q.back("note", "vatcheck.accounts_saved"), http.StatusSeeOther)
}

func (d Deps) handleVATGuess(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := vatQueryOf(r)
	n, err := vatcheck.Guess(r.Context(), d.DB, ctx.Who, q.Scope, q.Span, time.Now().UTC())
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

func (d Deps) handleVATFiled(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := vatQueryOf(r)
	if err := vatcheck.FileReturn(r.Context(), d.DB, ctx.Who, q.Scope, q.Span, time.Now().UTC(), d.clientIP(r)); err != nil {
		http.Redirect(w, r, q.back("error", vatError(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, q.back("note", "vatcheck.filed_note")+"#elster", http.StatusSeeOther)
}
