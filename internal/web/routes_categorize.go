package web

// Sure's bookings without category filed (services/categorize):
//
//	GET  /categorize          merchants with bookings to file
//	POST /categorize/review   the ticked filings, spelled out
//	POST /categorize/apply    files the bookings in Sure

import (
	"net/http"
	"net/url"
	"strconv"

	"andon/internal/services/categorize"
)

func (d Deps) registerCategorizeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /categorize", d.authed(d.handleCategorize))
	mux.HandleFunc("POST /categorize/review", d.authed(d.handleCategorizeReview))
	mux.HandleFunc("POST /categorize/apply", d.authed(d.handleCategorizeApply))
}

func categorizeBack(scope string, pairs ...string) string {
	v := url.Values{}
	if scope != "" {
		v.Set(benchScope, scope)
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		v.Set(pairs[i], pairs[i+1])
	}
	return "/categorize?" + v.Encode()
}

func (d Deps) handleCategorize(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	page, err := categorize.Open(r.Context(), d.DB, ctx.Who, r.FormValue(benchScope))
	query := r.URL.Query()
	values := map[string]any{"P": page, "Error": query.Get("error"), "FailedName": query.Get("who"), "Reason": query.Get("reason")}
	for _, k := range []string{"done", "failed"} {
		values[k], _ = strconv.Atoi(query.Get(k))
	}
	if err != nil {
		values["Error"] = errKey(err)
	}
	_ = d.Page(w, ctx, "categorize", http.StatusOK, values)
}

func (d Deps) handleCategorizeReview(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	scope := r.FormValue(benchScope)
	plan, err := categorize.Review(r.Context(), d.DB, ctx.Who, scope, benchSteps(r.PostForm, categorize.Fields))
	if err != nil {
		http.Redirect(w, r, categorizeBack(scope, "error", errKey(err)), http.StatusSeeOther)
		return
	}
	_ = d.Page(w, ctx, "categorize_review", http.StatusOK, map[string]any{"Plan": plan})
}

func (d Deps) handleCategorizeApply(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	scope := r.FormValue(benchScope)
	res, err := categorize.Apply(r.Context(), d.DB, ctx.Who, scope, benchSteps(r.PostForm, categorize.Fields), d.clientIP(r))
	if err != nil {
		http.Redirect(w, r, categorizeBack(scope, "error", errKey(err)), http.StatusSeeOther)
		return
	}
	pairs := []string{"done", strconv.Itoa(res.Done)}
	if f := res.Failed; len(f) > 0 {
		pairs = append(pairs, "failed", strconv.Itoa(len(f)), "who", f[0].Name, "reason", f[0].Reason)
	}
	http.Redirect(w, r, categorizeBack(scope, pairs...), http.StatusSeeOther)
}
