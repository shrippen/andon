package web

// The network's devices brought into Snipe-IT (services/inventory):
//
//	GET  /inventory          devices no asset is named like
//	POST /inventory/review   the ticked assets and ignores, spelled out
//	POST /inventory/apply    adds them to Snipe-IT, extends the ignore list

import (
	"net/http"
	"net/url"
	"strconv"

	"andon/internal/services/inventory"
)

func (d Deps) registerInventoryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /inventory", d.authed(d.handleInventory))
	mux.HandleFunc("POST /inventory/review", d.authed(d.handleInventoryReview))
	mux.HandleFunc("POST /inventory/apply", d.authed(d.handleInventoryApply))
}

func inventoryBack(scope string, pairs ...string) string {
	v := url.Values{}
	if scope != "" {
		v.Set(benchScope, scope)
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		v.Set(pairs[i], pairs[i+1])
	}
	return "/inventory?" + v.Encode()
}

func (d Deps) handleInventory(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	page, err := inventory.Open(r.Context(), d.DB, ctx.Who, r.FormValue(benchScope))
	query := r.URL.Query()
	values := map[string]any{"P": page, "Error": query.Get("error"), "Who": query.Get("who"), "Reason": query.Get("reason")}
	for _, k := range []string{"done", "failed"} {
		values[k], _ = strconv.Atoi(query.Get(k))
	}
	if err != nil {
		values["Error"] = errKey(err)
	}
	_ = d.Page(w, ctx, "inventory", http.StatusOK, values)
}

func (d Deps) handleInventoryReview(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	scope := r.FormValue(benchScope)
	plan, err := inventory.Review(r.Context(), d.DB, ctx.Who, scope, benchSteps(r.PostForm, inventory.Fields))
	if err != nil {
		http.Redirect(w, r, inventoryBack(scope, "error", errKey(err)), http.StatusSeeOther)
		return
	}
	_ = d.Page(w, ctx, "inventory_review", http.StatusOK, map[string]any{"Plan": plan})
}

func (d Deps) handleInventoryApply(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	scope := r.FormValue(benchScope)
	res, err := inventory.Apply(r.Context(), d.DB, ctx.Who, scope, benchSteps(r.PostForm, inventory.Fields), d.clientIP(r))
	if err != nil {
		http.Redirect(w, r, inventoryBack(scope, "error", errKey(err)), http.StatusSeeOther)
		return
	}
	pairs := []string{"done", strconv.Itoa(res.Done)}
	if f := res.Failed; len(f) > 0 {
		pairs = append(pairs, "failed", strconv.Itoa(len(f)), "who", f[0].Name, "reason", f[0].Reason)
	}
	http.Redirect(w, r, inventoryBack(scope, pairs...), http.StatusSeeOther)
}
