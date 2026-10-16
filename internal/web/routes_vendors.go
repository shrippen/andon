package web

// Duplicate vendors merged (services/vendors):
//
//	GET  /vendors          groups of vendors with the same first word
//	POST /vendors/review   the merges ticked, spelled out
//	POST /vendors/apply    moves the expenses, archives the duplicates

import (
	"net/http"
	"net/url"
	"strconv"

	"andon/internal/services/vendors"
)

func (d Deps) registerVendorRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /vendors", d.authed(d.handleVendors))
	mux.HandleFunc("POST /vendors/review", d.authed(d.handleVendorsReview))
	mux.HandleFunc("POST /vendors/apply", d.authed(d.handleVendorsApply))
}

func vendorsBack(scope string, pairs ...string) string {
	v := url.Values{}
	if scope != "" {
		v.Set(benchScope, scope)
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		v.Set(pairs[i], pairs[i+1])
	}
	return "/vendors?" + v.Encode()
}

func (d Deps) handleVendors(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	page, err := vendors.Open(r.Context(), d.DB, ctx.Who, r.FormValue(benchScope))
	query := r.URL.Query()
	values := map[string]any{"P": page, "Error": query.Get("error"), "FailedName": query.Get("who"), "Reason": query.Get("reason")}
	for _, k := range []string{"done", "failed"} {
		values[k], _ = strconv.Atoi(query.Get(k))
	}
	if err != nil {
		values["Error"] = errKey(err)
	}
	_ = d.Page(w, ctx, "vendors", http.StatusOK, values)
}

func (d Deps) handleVendorsReview(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	scope := r.FormValue(benchScope)
	plan, err := vendors.Review(r.Context(), d.DB, ctx.Who, scope, benchSteps(r.PostForm, vendors.Fields))
	if err != nil {
		http.Redirect(w, r, vendorsBack(scope, "error", errKey(err)), http.StatusSeeOther)
		return
	}
	_ = d.Page(w, ctx, "vendors_review", http.StatusOK, map[string]any{"Plan": plan})
}

func (d Deps) handleVendorsApply(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	scope := r.FormValue(benchScope)
	res, err := vendors.Apply(r.Context(), d.DB, ctx.Who, scope, benchSteps(r.PostForm, vendors.Fields), d.clientIP(r))
	if err != nil {
		http.Redirect(w, r, vendorsBack(scope, "error", errKey(err)), http.StatusSeeOther)
		return
	}
	pairs := []string{"done", strconv.Itoa(res.Done)}
	if f := res.Failed; len(f) > 0 {
		pairs = append(pairs, "failed", strconv.Itoa(len(f)), "who", f[0].Name, "reason", f[0].Reason)
	}
	http.Redirect(w, r, vendorsBack(scope, pairs...), http.StatusSeeOther)
}
