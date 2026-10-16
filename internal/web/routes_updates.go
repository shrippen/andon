package web

// Image updates proposed to the compose repos (services/updates):
//
//	GET  /updates          newer tags by urgency
//	POST /updates/review   the ticked proposals, spelled out
//	POST /updates/apply    opens a pull request per stack in Gitea
//	POST /updates/summarize  Claude sums up one update's release notes

import (
	"net/http"
	"net/url"
	"strconv"

	"andon/internal/services/updates"
)

func (d Deps) registerUpdateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /updates", d.authed(d.handleUpdates))
	mux.HandleFunc("POST /updates/review", d.authed(d.handleUpdatesReview))
	mux.HandleFunc("POST /updates/apply", d.authed(d.handleUpdatesApply))
	mux.HandleFunc("POST /updates/summarize", d.authed(d.handleUpdatesSummarize))
}

func updatesBack(scope string, pairs ...string) string {
	v := url.Values{}
	if scope != "" {
		v.Set(benchScope, scope)
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		v.Set(pairs[i], pairs[i+1])
	}
	return "/updates?" + v.Encode()
}

func (d Deps) handleUpdates(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	page, err := updates.Open(r.Context(), d.DB, ctx.Who, r.FormValue(benchScope))
	query := r.URL.Query()
	values := map[string]any{"P": page, "Error": query.Get("error"), "FailedName": query.Get("who"), "Reason": query.Get("reason"), "PR": safePR(query.Get("pr"))}
	for _, k := range []string{"done", "failed"} {
		values[k], _ = strconv.Atoi(query.Get(k))
	}
	if err != nil {
		values["Error"] = errKey(err)
	}
	_ = d.Page(w, ctx, "updates", http.StatusOK, values)
}

func (d Deps) handleUpdatesReview(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	scope := r.FormValue(benchScope)
	plan, err := updates.Review(r.Context(), d.DB, ctx.Who, scope, benchSteps(r.PostForm, updates.Fields))
	if err != nil {
		http.Redirect(w, r, updatesBack(scope, "error", errKey(err)), http.StatusSeeOther)
		return
	}
	_ = d.Page(w, ctx, "updates_review", http.StatusOK, map[string]any{"Plan": plan})
}

func (d Deps) handleUpdatesApply(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	scope := r.FormValue(benchScope)
	res, opened, err := updates.Apply(r.Context(), d.DB, ctx.Who, scope, benchSteps(r.PostForm, updates.Fields), d.clientIP(r))
	if err != nil {
		http.Redirect(w, r, updatesBack(scope, "error", errKey(err)), http.StatusSeeOther)
		return
	}
	pairs := []string{"done", strconv.Itoa(res.Done)}
	if len(opened) > 0 {
		pairs = append(pairs, "pr", opened[0])
	}
	if f := res.Failed; len(f) > 0 {
		pairs = append(pairs, "failed", strconv.Itoa(len(f)), "who", f[0].Name, "reason", f[0].Reason)
	}
	http.Redirect(w, r, updatesBack(scope, pairs...), http.StatusSeeOther)
}

// safePR keeps only a web link: the query comes back from the browser.
func safePR(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
		return ""
	}
	return u.String()
}

func (d Deps) handleUpdatesSummarize(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	scope, key := r.FormValue(benchScope), r.FormValue("case")
	if err := updates.Summarize(r.Context(), d.DB, ctx.Who, scope, key); err != nil {
		http.Redirect(w, r, updatesBack(scope, "error", errKey(err)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, updatesBack(scope)+"#"+key, http.StatusSeeOther)
}
