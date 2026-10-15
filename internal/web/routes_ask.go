package web

// "Frag Andon" (services/ask, experimental, reads only):
//
//	GET  /ask   the question form, and what goes out with it
//	POST /ask   Claude's answer on the same page

import (
	"net/http"

	"andon/internal/services/ask"
	"andon/internal/services/assist"
)

func (d Deps) registerAskRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /ask", d.authed(d.handleAsk))
	mux.HandleFunc("POST /ask", d.authed(d.handleAsk))
}

func (d Deps) handleAsk(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	values := map[string]any{"On": assist.Enabled(), "Question": r.FormValue("question")}
	if r.Method == http.MethodPost {
		answer, err := ask.Answer(r.Context(), d.DB, ctx.Who, r.FormValue("question"))
		values["Answer"] = answer
		if err != nil {
			values["Error"] = errKey(err)
		}
	}
	_ = d.Page(w, ctx, "ask", http.StatusOK, values)
}
