package web

// The sync tab of a Homelable connection's record: the last sync's log
// and "Sync now" (services/homelable).

import (
	"net/http"
	"net/url"
	"strconv"

	"andon/internal/services/homelable"
)

// tabSync is the sync tab of a Homelable record.
const tabSync = "sync"

// handleHomelableSync syncs now and lands on the tab with the counts.
func (d Deps) handleHomelableSync(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	q := url.Values{"tab": {tabSync}}
	log, err := homelable.Sync(r.Context(), d.DB, ctx.Who, id)
	switch {
	case err != nil:
		q.Set("error", errKey(err))
	case log.Problem == "":
		q.Set("note", "synced")
	}
	http.Redirect(w, r, "/connections/"+strconv.FormatInt(id, 10)+"?"+q.Encode(), http.StatusSeeOther)
}
