package web

// Closing a week in Kimai (services/week):
//
//	GET  /week          the week's gaps with proposals, ?week=2026-09-07
//	POST /week/review   the ticked entries, spelled out
//	POST /week/apply    books them in Kimai

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"andon/internal/services/week"
)

func (d Deps) registerWeekRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /week", d.authed(d.handleWeek))
	mux.HandleFunc("POST /week/review", d.authed(d.handleWeekReview))
	mux.HandleFunc("POST /week/apply", d.authed(d.handleWeekApply))
}

type weekQuery struct {
	Scope string
	Week  time.Time
}

func weekQueryOf(r *http.Request) weekQuery {
	w, _ := time.Parse(time.DateOnly, r.FormValue("week"))
	return weekQuery{Scope: r.FormValue(benchScope), Week: w}
}

func (q weekQuery) back(pairs ...string) string {
	v := url.Values{}
	if q.Scope != "" {
		v.Set(benchScope, q.Scope)
	}
	if !q.Week.IsZero() {
		v.Set("week", q.Week.Format(time.DateOnly))
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		v.Set(pairs[i], pairs[i+1])
	}
	return "/week?" + v.Encode()
}

func (d Deps) handleWeek(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := weekQueryOf(r)
	page, err := week.Open(r.Context(), d.DB, ctx.Who, q.Scope, q.Week, time.Now())
	query := r.URL.Query()
	values := map[string]any{"P": page, "Error": query.Get("error"), "FailedName": query.Get("who"), "Reason": query.Get("reason")}
	for _, k := range []string{"done", "failed"} {
		values[k], _ = strconv.Atoi(query.Get(k))
	}
	if err != nil {
		values["Error"] = errKey(err)
	}
	_ = d.Page(w, ctx, "week", http.StatusOK, values)
}

func (d Deps) handleWeekReview(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := weekQueryOf(r)
	plan, err := week.Review(r.Context(), d.DB, ctx.Who, q.Scope, q.Week, benchSteps(r.PostForm, week.Fields))
	if err != nil {
		http.Redirect(w, r, q.back("error", errKey(err)), http.StatusSeeOther)
		return
	}
	_ = d.Page(w, ctx, "week_review", http.StatusOK, map[string]any{"Plan": plan})
}

func (d Deps) handleWeekApply(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := weekQueryOf(r)
	res, err := week.Apply(r.Context(), d.DB, ctx.Who, q.Scope, q.Week, benchSteps(r.PostForm, week.Fields), d.clientIP(r))
	if err != nil {
		http.Redirect(w, r, q.back("error", errKey(err)), http.StatusSeeOther)
		return
	}
	pairs := []string{"done", strconv.Itoa(res.Done)}
	if f := res.Failed; len(f) > 0 {
		pairs = append(pairs, "failed", strconv.Itoa(len(f)), "who", f[0].Name, "reason", f[0].Reason)
	}
	http.Redirect(w, r, q.back(pairs...), http.StatusSeeOther)
}
