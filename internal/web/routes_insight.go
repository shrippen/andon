package web

import (
	"net/http"
	"time"

	"andon/internal/services/history"
	"andon/internal/services/reports"
)

const (
	timelineDays  = 14
	timelineLimit = 200
)

// RegisterInsightRoutes wires the timeline and the provider report.
func (d Deps) RegisterInsightRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /timeline", d.authed(d.handleTimeline))
	mux.HandleFunc("GET /reports/isp", d.authed(d.handleISPReport))
	mux.HandleFunc("GET /reports/isp.csv", d.authed(d.handleISPCSV))
}

// handleTimeline lists updates and hints that came or went.
func (d Deps) handleTimeline(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	entries, err := history.Timeline(d.DB, ctx.Who, time.Now().UTC().AddDate(0, 0, -timelineDays), timelineLimit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = d.Page(w, ctx, "timeline", http.StatusOK, map[string]any{"Entries": entries, "Days": timelineDays})
}

func (d Deps) handleISPReport(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	list, err := reports.ISPReports(r.Context(), d.DB, ctx.Who)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = d.Page(w, ctx, "isp_report", http.StatusOK, map[string]any{"Reports": list, "Days": reports.ISPDays,
		"Share": int(reports.ISPShare * 100)})
}

func (d Deps) handleISPCSV(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	list, err := reports.ISPReports(r.Context(), d.DB, ctx.Who)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	blob, err := reports.ISPCSV(list)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="internet-`+time.Now().Format("2006-01")+`.csv"`)
	w.Write(blob)
}
