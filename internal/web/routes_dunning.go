package web

// Payment reminders (services/dunning):
//
//	GET  /dunning          invoices due for their next reminder
//	POST /dunning/draft    Claude writes the texts still missing
//	POST /dunning/review   the ticked reminders, spelled out
//	POST /dunning/apply    Invoice Ninja mails them

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"andon/internal/services/dunning"
)

func (d Deps) registerDunningRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /dunning", d.authed(d.handleDunning))
	mux.HandleFunc("POST /dunning/draft", d.authed(d.handleDunningDraft))
	mux.HandleFunc("POST /dunning/review", d.authed(d.handleDunningReview))
	mux.HandleFunc("POST /dunning/apply", d.authed(d.handleDunningApply))
}

func dunningBack(scope string, pairs ...string) string {
	v := url.Values{}
	if scope != "" {
		v.Set(benchScope, scope)
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		v.Set(pairs[i], pairs[i+1])
	}
	return "/dunning?" + v.Encode()
}

func (d Deps) handleDunning(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	page, err := dunning.Open(r.Context(), d.DB, ctx.Who, r.FormValue(benchScope), time.Now())
	query := r.URL.Query()
	values := map[string]any{"P": page, "Error": query.Get("error"), "FailedName": query.Get("who"), "Reason": query.Get("reason")}
	for _, k := range []string{"done", "failed", "drafted"} {
		values[k], _ = strconv.Atoi(query.Get(k))
	}
	if err != nil {
		values["Error"] = errKey(err)
	}
	_ = d.Page(w, ctx, "dunning", http.StatusOK, values)
}

func (d Deps) handleDunningDraft(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	scope := r.FormValue(benchScope)
	n, err := dunning.Draft(r.Context(), d.DB, ctx.Who, scope, time.Now())
	if err != nil {
		http.Redirect(w, r, dunningBack(scope, "error", errKey(err), "drafted", strconv.Itoa(n)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, dunningBack(scope, "drafted", strconv.Itoa(n)), http.StatusSeeOther)
}

func (d Deps) handleDunningReview(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	scope := r.FormValue(benchScope)
	plan, err := dunning.Review(r.Context(), d.DB, ctx.Who, scope, benchSteps(r.PostForm, dunning.Fields), time.Now())
	if err != nil {
		http.Redirect(w, r, dunningBack(scope, "error", errKey(err)), http.StatusSeeOther)
		return
	}
	_ = d.Page(w, ctx, "dunning_review", http.StatusOK, map[string]any{"Plan": plan})
}

func (d Deps) handleDunningApply(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	scope := r.FormValue(benchScope)
	res, err := dunning.Apply(r.Context(), d.DB, ctx.Who, scope, benchSteps(r.PostForm, dunning.Fields), time.Now(), d.clientIP(r))
	if err != nil {
		http.Redirect(w, r, dunningBack(scope, "error", errKey(err)), http.StatusSeeOther)
		return
	}
	pairs := []string{"done", strconv.Itoa(res.Done)}
	if f := res.Failed; len(f) > 0 {
		pairs = append(pairs, "failed", strconv.Itoa(len(f)), "who", f[0].Name, "reason", f[0].Reason)
	}
	http.Redirect(w, r, dunningBack(scope, pairs...), http.StatusSeeOther)
}
