package web

// Verbünde of a space: connections of different services that work
// together (services/verbund). One page per space lists them; forms
// create, rename, change members and delete.

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"andon/internal/enums"
	"andon/internal/services/connections"
	"andon/internal/services/spaces"
	"andon/internal/services/verbund"
)

// RegisterVerbundRoutes wires the Verbünde page and its forms.
func (d Deps) RegisterVerbundRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /spaces/{id}/verbund", d.authed(d.handleVerbundPage))
	mux.HandleFunc("POST /verbund", d.authed(d.handleVerbundCreate))
	mux.HandleFunc("POST /verbund/{id}/rename", d.authed(d.handleVerbundRename))
	mux.HandleFunc("POST /verbund/{id}/members", d.authed(d.handleVerbundAdd))
	mux.HandleFunc("POST /verbund/{id}/members/{conn}/remove", d.authed(d.handleVerbundRemove))
	mux.HandleFunc("POST /verbund/{id}/delete", d.authed(d.handleVerbundDelete))
	mux.HandleFunc("GET /verbund/{id}/customers", d.authed(d.handleVerbundCustomers))
	mux.HandleFunc("POST /verbund/{id}/customers/confirm", d.authed(d.handleCustomersConfirm))
	mux.HandleFunc("POST /verbund/{id}/customers/{customer}/{act}", d.authed(d.handleCustomerAct))
}

// customersPath is a Verbund's customers page.
func customersPath(id, spaceID int64) string {
	return "/verbund/" + strconv.FormatInt(id, 10) + "/customers?space=" + strconv.FormatInt(spaceID, 10)
}

// handleVerbundCustomers shows which Kimai customer is which Invoice
// Ninja client, with suggestions.
func (d Deps) handleVerbundCustomers(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	space, _ := strconv.ParseInt(r.URL.Query().Get("space"), 10, 64)
	values := map[string]any{"SpaceID": space, "Error": r.URL.Query().Get("error")}
	view, err := verbund.Customers(r.Context(), d.DB, ctx.Who, id)
	switch {
	case errors.Is(err, verbund.ErrNotFound):
		http.NotFound(w, r)
		return
	case err != nil:
		values["Error"] = errKey(err)
		if v, gerr := verbund.Get(d.DB, ctx.Who, id); gerr == nil {
			view.Verbund = v
		}
	}
	values["View"] = view
	if _, ok := ctx.Who.Spaces[space]; ok {
		if err := d.levelValues(ctx, space, values); err != nil {
			d.fail(w, err, http.StatusInternalServerError)
			return
		}
		values[navPath] = verbundPath(space)
	}
	_ = d.Page(w, ctx, "verbund_customers", http.StatusOK, values)
}

// customersBack returns to the customers page, with an error key if any.
func (d Deps) customersBack(w http.ResponseWriter, r *http.Request, id int64, err error) {
	space, _ := strconv.ParseInt(r.FormValue("space"), 10, 64)
	target := customersPath(id, space)
	if err != nil {
		target += "&error=" + errKey(err)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (d Deps) handleCustomersConfirm(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, err = verbund.ConfirmSuggestions(r.Context(), d.DB, ctx.Who, id, d.clientIP(r))
	d.customersBack(w, r, id, err)
}

// customerActs are what a row's buttons do: link (form value client,
// "" = none), unlink, align the Kimai name to Ninja's.
func (d Deps) handleCustomerAct(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err1 := pathID(r, "id")
	customer, err2 := pathID(r, "customer")
	if err1 != nil || err2 != nil {
		http.NotFound(w, r)
		return
	}
	var err error
	switch r.PathValue("act") {
	case "link":
		err = verbund.LinkCustomer(r.Context(), d.DB, ctx.Who, id, customer, r.FormValue("client"), d.clientIP(r))
	case "none":
		err = verbund.LinkCustomer(r.Context(), d.DB, ctx.Who, id, customer, "", d.clientIP(r))
	case "unlink":
		err = verbund.UnlinkCustomer(d.DB, ctx.Who, id, customer, d.clientIP(r))
	case "align":
		err = verbund.AlignName(r.Context(), d.DB, ctx.Who, id, customer, d.clientIP(r))
	default:
		http.NotFound(w, r)
		return
	}
	d.customersBack(w, r, id, err)
}

// verbundPath is a space's Verbünde page.
func verbundPath(spaceID int64) string { return spacePath(spaceID) + "/verbund" }

func (d Deps) handleVerbundPage(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if _, known := ctx.Who.Spaces[id]; err != nil || !known || spaces.OpenSettings(d.DB, ctx.Who, id) != nil {
		http.NotFound(w, r)
		return
	}
	d.verbundPage(w, r, ctx, id, http.StatusOK, r.URL.Query().Get("error"))
}

// verbundPage shows the space's Verbünde, the services still ambiguous,
// and the connections a new Verbund may take.
func (d Deps) verbundPage(w http.ResponseWriter, r *http.Request, ctx Ctx, spaceID int64, status int, errKeyText string) {
	list, err := verbund.List(d.DB, ctx.Who, spaceID)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	vague, err := verbund.Ambiguous(d.DB, ctx.Who, spaceID)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	editable, err := connections.Listing(d.DB, ctx.Who, enums.RightEdit)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	// The space's own connections first, then the others the caller edits.
	ordered := make([]connections.View, 0, len(editable))
	for _, own := range []bool{true, false} {
		for _, c := range editable {
			if (c.SpaceID == spaceID) == own {
				ordered = append(ordered, c)
			}
		}
	}
	values := map[string]any{"Verbuende": list, "Vague": vague, "Editable": ordered, "SpaceID": spaceID, "Error": errKeyText}
	if err := d.levelValues(ctx, spaceID, values); err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	values[navPath] = verbundPath(spaceID)
	_ = d.Page(w, ctx, "verbund", status, values)
}

// verbundBack returns to the space's page, with an error key if any.
func (d Deps) verbundBack(w http.ResponseWriter, r *http.Request, err error) {
	space, _ := strconv.ParseInt(r.FormValue("space"), 10, 64)
	target := verbundPath(space)
	if err != nil {
		target += "?error=" + errKey(err)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func (d Deps) handleVerbundCreate(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	if err := r.ParseForm(); err != nil {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	var ids []int64
	for _, raw := range r.Form["conn"] {
		if id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	_, err := verbund.Create(d.DB, ctx.Who, r.FormValue("name"), ids, d.clientIP(r))
	d.verbundBack(w, r, err)
}

func (d Deps) handleVerbundRename(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d.verbundBack(w, r, verbund.Rename(d.DB, ctx.Who, id, r.FormValue("name"), d.clientIP(r)))
}

func (d Deps) handleVerbundAdd(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d.verbundBack(w, r, verbund.AddMember(d.DB, ctx.Who, id, formID(r, "conn"), d.clientIP(r)))
}

func (d Deps) handleVerbundRemove(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err1 := pathID(r, "id")
	conn, err2 := pathID(r, "conn")
	if err1 != nil || err2 != nil {
		http.NotFound(w, r)
		return
	}
	d.verbundBack(w, r, verbund.RemoveMember(d.DB, ctx.Who, id, conn, d.clientIP(r)))
}

func (d Deps) handleVerbundDelete(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d.verbundBack(w, r, verbund.Delete(d.DB, ctx.Who, id, d.clientIP(r)))
}
