package web

// Subscriptions brought in line with the bank (services/subs):
//
//	GET  /subs          Sure's payments against Wallos and Invoice Ninja
//	POST /subs/review   the ticked changes, spelled out
//	POST /subs/apply    writes them to Wallos or Invoice Ninja

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"andon/internal/services/subs"
)

func (d Deps) registerSubsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /subs", d.authed(d.handleSubs))
	mux.HandleFunc("POST /subs/review", d.authed(d.handleSubsReview))
	mux.HandleFunc("POST /subs/apply", d.authed(d.handleSubsApply))
}

func subsBack(scope string, pairs ...string) string {
	v := url.Values{}
	if scope != "" {
		v.Set(benchScope, scope)
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		v.Set(pairs[i], pairs[i+1])
	}
	return "/subs?" + v.Encode()
}

func (d Deps) handleSubs(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	page, err := subs.Open(r.Context(), d.DB, ctx.Who, r.FormValue(benchScope), time.Now())
	query := r.URL.Query()
	values := map[string]any{"P": page, "Error": query.Get("error"), "FailedName": query.Get("who"), "Reason": query.Get("reason")}
	for _, k := range []string{"done", "failed"} {
		values[k], _ = strconv.Atoi(query.Get(k))
	}
	if err != nil {
		values["Error"] = errKey(err)
	}
	_ = d.Page(w, ctx, "subs", http.StatusOK, values)
}

func (d Deps) handleSubsReview(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	scope := r.FormValue(benchScope)
	plan, err := subs.Review(r.Context(), d.DB, ctx.Who, scope, time.Now(), benchSteps(r.PostForm, subs.Fields))
	if err != nil {
		http.Redirect(w, r, subsBack(scope, "error", errKey(err)), http.StatusSeeOther)
		return
	}
	_ = d.Page(w, ctx, "subs_review", http.StatusOK, map[string]any{"Plan": plan})
}

func (d Deps) handleSubsApply(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	scope := r.FormValue(benchScope)
	res, err := subs.Apply(r.Context(), d.DB, ctx.Who, scope, time.Now(), benchSteps(r.PostForm, subs.Fields), d.clientIP(r))
	if err != nil {
		http.Redirect(w, r, subsBack(scope, "error", errKey(err)), http.StatusSeeOther)
		return
	}
	pairs := []string{"done", strconv.Itoa(res.Done)}
	if f := res.Failed; len(f) > 0 {
		pairs = append(pairs, "failed", strconv.Itoa(len(f)), "who", f[0].Name, "reason", f[0].Reason)
	}
	http.Redirect(w, r, subsBack(scope, pairs...), http.StatusSeeOther)
}
