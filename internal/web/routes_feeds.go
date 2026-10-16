package web

// Release feeds kept in line with the stacks (services/feeds):
//
//	GET  /feeds          feeds to subscribe or remove
//	POST /feeds/review   the ticked changes, spelled out
//	POST /feeds/apply    writes them to FreshRSS

import (
	"net/http"
	"net/url"
	"strconv"

	"andon/internal/services/feeds"
)

func (d Deps) registerFeedRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /feeds", d.authed(d.handleFeeds))
	mux.HandleFunc("POST /feeds/review", d.authed(d.handleFeedsReview))
	mux.HandleFunc("POST /feeds/apply", d.authed(d.handleFeedsApply))
}

func feedsBack(scope string, pairs ...string) string {
	v := url.Values{}
	if scope != "" {
		v.Set(benchScope, scope)
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		v.Set(pairs[i], pairs[i+1])
	}
	return "/feeds?" + v.Encode()
}

func (d Deps) handleFeeds(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	page, err := feeds.Open(r.Context(), d.DB, ctx.Who, r.FormValue(benchScope))
	query := r.URL.Query()
	values := map[string]any{"P": page, "Error": query.Get("error"), "FailedName": query.Get("who"), "Reason": query.Get("reason")}
	for _, k := range []string{"done", "failed"} {
		values[k], _ = strconv.Atoi(query.Get(k))
	}
	if err != nil {
		values["Error"] = errKey(err)
	}
	_ = d.Page(w, ctx, "feeds", http.StatusOK, values)
}

func (d Deps) handleFeedsReview(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	scope := r.FormValue(benchScope)
	plan, err := feeds.Review(r.Context(), d.DB, ctx.Who, scope, benchSteps(r.PostForm, feeds.Fields))
	if err != nil {
		http.Redirect(w, r, feedsBack(scope, "error", errKey(err)), http.StatusSeeOther)
		return
	}
	_ = d.Page(w, ctx, "feeds_review", http.StatusOK, map[string]any{"Plan": plan})
}

func (d Deps) handleFeedsApply(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	scope := r.FormValue(benchScope)
	res, err := feeds.Apply(r.Context(), d.DB, ctx.Who, scope, benchSteps(r.PostForm, feeds.Fields), d.clientIP(r))
	if err != nil {
		http.Redirect(w, r, feedsBack(scope, "error", errKey(err)), http.StatusSeeOther)
		return
	}
	pairs := []string{"done", strconv.Itoa(res.Done)}
	if f := res.Failed; len(f) > 0 {
		pairs = append(pairs, "failed", strconv.Itoa(len(f)), "who", f[0].Name, "reason", f[0].Reason)
	}
	http.Redirect(w, r, feedsBack(scope, pairs...), http.StatusSeeOther)
}
