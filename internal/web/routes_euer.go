package web

// The EÜR workbench fills the EÜR line of expenses (services/euer):
//
//	GET  /euer          the year's expenses with an empty EÜR field
//	POST /euer/review   the ticked ones, spelled out
//	POST /euer/apply    writes them

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"andon/internal/services/euer"
)

func (d Deps) registerEuerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /euer", d.authed(d.handleEuer))
	mux.HandleFunc("POST /euer/review", d.authed(d.handleEuerReview))
	mux.HandleFunc("POST /euer/apply", d.authed(d.handleEuerApply))
}

// euerQuery is the Verbund and year carried in links and forms.
type euerQuery struct {
	Scope string
	Year  int
}

func euerQueryOf(r *http.Request) euerQuery {
	y, _ := strconv.Atoi(r.FormValue("year"))
	return euerQuery{Scope: r.FormValue(benchScope), Year: y}
}

func (q euerQuery) back(pairs ...string) string {
	v := url.Values{}
	if q.Scope != "" {
		v.Set(benchScope, q.Scope)
	}
	if q.Year != 0 {
		v.Set("year", strconv.Itoa(q.Year))
	}
	for i := 0; i+1 < len(pairs); i += 2 {
		v.Set(pairs[i], pairs[i+1])
	}
	return "/euer?" + v.Encode()
}

func (d Deps) handleEuer(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := euerQueryOf(r)
	page, err := euer.Open(r.Context(), d.DB, ctx.Who, q.Scope, q.Year, time.Now())
	query := r.URL.Query()
	values := map[string]any{"P": page, "Error": query.Get("error"), "Who": query.Get("who"), "Reason": query.Get("reason")}
	for _, k := range []string{"done", "failed"} {
		values[k], _ = strconv.Atoi(query.Get(k))
	}
	if err != nil {
		values["Error"] = errKey(err)
	}
	_ = d.Page(w, ctx, "euer", http.StatusOK, values)
}

func (d Deps) handleEuerReview(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := euerQueryOf(r)
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	plan, err := euer.Review(r.Context(), d.DB, ctx.Who, q.Scope, q.Year, benchSteps(r.PostForm, euer.Fields))
	if err != nil {
		http.Redirect(w, r, q.back("error", errKey(err)), http.StatusSeeOther)
		return
	}
	_ = d.Page(w, ctx, "euer_review", http.StatusOK, map[string]any{"Plan": plan, "Q": q})
}

func (d Deps) handleEuerApply(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	q := euerQueryOf(r)
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	res, err := euer.Apply(r.Context(), d.DB, ctx.Who, q.Scope, q.Year, benchSteps(r.PostForm, euer.Fields), d.clientIP(r))
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
