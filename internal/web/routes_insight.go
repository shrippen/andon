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
	// timelineStep is one bar of the density band: 4 per day.
	timelineStep = 6 * time.Hour
)

// RegisterInsightRoutes wires the timeline and the provider report.
func (d Deps) RegisterInsightRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /timeline", d.authed(d.handleTimeline))
	mux.HandleFunc("GET /reports/isp", d.authed(d.handleISPReport))
	mux.HandleFunc("GET /reports/isp.csv", d.authed(d.handleISPCSV))
}

// handleTimeline lists updates and hints that came or went.
func (d Deps) handleTimeline(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	now := time.Now()
	since := now.AddDate(0, 0, -timelineDays)
	entries, err := history.Timeline(d.DB, ctx.Who, since.UTC(), timelineLimit)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
	_ = d.Page(w, ctx, "timeline", http.StatusOK, map[string]any{
		"Groups": history.ByDay(entries, time.Local), "Band": history.Band(entries, since, now, timelineStep),
		"Since": since, "Today": today, "Yesterday": today.AddDate(0, 0, -1), "Days": timelineDays,
	})
}

func (d Deps) handleISPReport(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	list, err := reports.ISPReports(r.Context(), d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	_ = d.Page(w, ctx, "isp_report", http.StatusOK, map[string]any{"Reports": list, "Days": reports.ISPDays,
		"Share": int(reports.ISPShare * 100)})
}

func (d Deps) handleISPCSV(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	list, err := reports.ISPReports(r.Context(), d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	blob, err := reports.ISPCSV(list)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="internet-`+time.Now().Format("2006-01")+`.csv"`)
	w.Write(blob)
}
