package web

// Recent purchases made complete (services/purchases):
//
//	GET  /purchases          assets of the last year with gaps
//	POST /purchases/review   the ticked warranties, spelled out
//	POST /purchases/apply    writes them to Snipe-IT

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"andon/internal/services/purchases"
)

func (d Deps) registerPurchaseRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /purchases", d.authed(d.handlePurchases))
	mux.HandleFunc("POST /purchases/review", d.authed(d.handlePurchasesReview))
	mux.HandleFunc("POST /purchases/apply", d.authed(d.handlePurchasesApply))
}

func purchasesBack(scope string, pairs ...string) string {
	v := url.Values{}
	if scope != "" {
		v.Set(benchScope, scope)
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		v.Set(pairs[i], pairs[i+1])
	}
	return "/purchases?" + v.Encode()
}

func (d Deps) handlePurchases(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	page, err := purchases.Open(r.Context(), d.DB, ctx.Who, r.FormValue(benchScope), time.Now())
	query := r.URL.Query()
	values := map[string]any{"P": page, "Error": query.Get("error"), "Who": query.Get("who"), "Reason": query.Get("reason")}
	for _, k := range []string{"done", "failed"} {
		values[k], _ = strconv.Atoi(query.Get(k))
	}
	if err != nil {
		values["Error"] = errKey(err)
	}
	_ = d.Page(w, ctx, "purchases", http.StatusOK, values)
}

func (d Deps) handlePurchasesReview(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	scope := r.FormValue(benchScope)
	plan, err := purchases.Review(r.Context(), d.DB, ctx.Who, scope, time.Now(), benchSteps(r.PostForm, purchases.Fields))
	if err != nil {
		http.Redirect(w, r, purchasesBack(scope, "error", errKey(err)), http.StatusSeeOther)
		return
	}
	_ = d.Page(w, ctx, "purchases_review", http.StatusOK, map[string]any{"Plan": plan})
}

func (d Deps) handlePurchasesApply(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	scope := r.FormValue(benchScope)
	res, err := purchases.Apply(r.Context(), d.DB, ctx.Who, scope, time.Now(), benchSteps(r.PostForm, purchases.Fields), d.clientIP(r))
	if err != nil {
		http.Redirect(w, r, purchasesBack(scope, "error", errKey(err)), http.StatusSeeOther)
		return
	}
	pairs := []string{"done", strconv.Itoa(res.Done)}
	if f := res.Failed; len(f) > 0 {
		pairs = append(pairs, "failed", strconv.Itoa(len(f)), "who", f[0].Name, "reason", f[0].Reason)
	}
	http.Redirect(w, r, purchasesBack(scope, pairs...), http.StatusSeeOther)
}
