package web

// The journal lists what Andon wrote to other services for the user:
//
//	GET  /journal            newest first, ?area=vat|receipts|… narrows
//	POST /journal/{id}/undo  takes one back, where the service lets it

import (
	"net/http"
	"net/url"
	"strconv"

	"andon/internal/services/journal"
)

func (d Deps) registerJournalRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /journal", d.authed(d.handleJournal))
	mux.HandleFunc("POST /journal/{id}/undo", d.authed(d.handleJournalUndo))
}

func (d Deps) handleJournal(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	area := journal.Area(r.FormValue("area"))
	list, err := journal.Entries(d.DB, ctx.Who, area)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	query := r.URL.Query()
	_ = d.Page(w, ctx, "journal", http.StatusOK, map[string]any{"Entries": list, "Area": area, "Areas": journal.Areas,
		"Note": query.Get("note"), "Error": query.Get("error")})
}

func (d Deps) handleJournalUndo(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	v := url.Values{}
	if area := r.FormValue("area"); area != "" {
		v.Set("area", area)
	}
	if err := journal.Undo(r.Context(), d.DB, ctx.Who, id, d.clientIP(r)); err != nil {
		v.Set("error", errKey(err))
	} else {
		v.Set("note", "journal.undone")
	}
	http.Redirect(w, r, "/journal?"+v.Encode(), http.StatusSeeOther)
}
