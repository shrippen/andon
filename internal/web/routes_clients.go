package web

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"andon/internal/metrics"
	"andon/internal/services/clients"
)

// RegisterClientRoutes wires the customer pages: every Kimai customer
// with invoices, hours and hints on one card.
func (d Deps) RegisterClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /clients", d.authed(d.handleClients))
	mux.HandleFunc("GET /clients/{space}/{id}", d.authed(d.handleClient))
}

func (d Deps) handleClients(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	period := metrics.PeriodOf(r.URL.Query().Get("period"))
	cards, err := clients.List(r.Context(), d.DB, ctx.Who, period)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	_ = d.Page(w, ctx, "clients", http.StatusOK, map[string]any{"Cards": cards, "Periods": metrics.Periods, "Span": period.Span(time.Now())})
}

func (d Deps) handleClient(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	spaceID, err1 := pathID(r, "space")
	id, err2 := pathID(r, "id")
	if err1 != nil || err2 != nil {
		http.NotFound(w, r)
		return
	}
	kimai, _ := strconv.ParseInt(r.URL.Query().Get("kimai"), 10, 64)
	detail, err := clients.One(r.Context(), d.DB, ctx.Who, spaceID, kimai, id, metrics.PeriodOf(r.URL.Query().Get("period")))
	if errors.Is(err, clients.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	_ = d.Page(w, ctx, "client", http.StatusOK, map[string]any{"C": detail, "Periods": metrics.Periods})
}
