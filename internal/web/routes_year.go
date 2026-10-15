package web

// The tax year prepared (services/year):
//
//	GET /year   income, expenses by EÜR line, depreciation, home office
//	            days, open gaps; ?year=2025

import (
	"net/http"
	"strconv"
	"time"

	"andon/internal/services/year"
)

func (d Deps) registerYearRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /year", d.authed(d.handleYear))
}

func (d Deps) handleYear(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	y, _ := strconv.Atoi(r.FormValue("year"))
	page, err := year.Open(r.Context(), d.DB, ctx.Who, r.FormValue(benchScope), y, time.Now())
	values := map[string]any{"P": page}
	if err != nil {
		values["Error"] = errKey(err)
	}
	_ = d.Page(w, ctx, "year", http.StatusOK, values)
}
