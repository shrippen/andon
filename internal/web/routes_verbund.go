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
	"andon/internal/services/verbund"
)

// RegisterVerbundRoutes wires the Verbünde page and its forms.
func (d Deps) RegisterVerbundRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /spaces/{id}/verbund", d.authed(d.handleVerbundPage))
	mux.HandleFunc("POST /spaces/{id}/verbund/settle", d.authed(d.handleVerbundSettle))
	mux.HandleFunc("POST /verbund", d.authed(d.handleVerbundCreate))
	mux.HandleFunc("POST /verbund/{id}/rename", d.authed(d.handleVerbundRename))
	mux.HandleFunc("POST /verbund/{id}/members", d.authed(d.handleVerbundAdd))
	mux.HandleFunc("POST /verbund/{id}/members/{conn}/remove", d.authed(d.handleVerbundRemove))
	mux.HandleFunc("POST /verbund/{id}/delete", d.authed(d.handleVerbundDelete))
	mux.HandleFunc("GET /verbund/{id}/customers", d.authed(d.handleVerbundCustomers))
	mux.HandleFunc("POST /verbund/{id}/customers/confirm", d.authed(d.handleCustomersConfirm))
	mux.HandleFunc("POST /verbund/{id}/customers/{act}", d.authed(d.handleCustomerAct))
}

// customersPath is a Verbund's customers page.
func customersPath(id, spaceID int64) string {
	return "/verbund/" + strconv.FormatInt(id, 10) + "/customers?space=" + strconv.FormatInt(spaceID, 10)
}

// handleVerbundCustomers shows each customer across the Verbund's
// members (Invoice Ninja, Kimai, Sure, Paperless), with suggestions.
func (d Deps) handleVerbundCustomers(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	space, _ := strconv.ParseInt(r.URL.Query().Get("space"), 10, 64)
	d.customersPage(w, r, ctx, id, space, http.StatusOK, "")
}

// customersPage renders a Verbund's customers; a refused cell shows its
// error and, through the page's refill (forms.go), the choice as made.
func (d Deps) customersPage(w http.ResponseWriter, r *http.Request, ctx Ctx, id, space int64, status int, errKeyText string) {
	values := map[string]any{"SpaceID": space, "Error": errKeyText}
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
	_ = d.Page(w, ctx, "verbund_customers", status, values)
}

// customersBack returns to the customers page; a refusal shows the page
// again with the error and the cell as chosen.
func (d Deps) customersBack(w http.ResponseWriter, r *http.Request, ctx Ctx, id int64, err error) {
	space, _ := strconv.ParseInt(r.FormValue("space"), 10, 64)
	if err == nil {
		http.Redirect(w, r, customersPath(id, space), http.StatusSeeOther)
		return
	}
	if isAny(err, deniedErrors) || isAny(err, notFoundErrors) {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	d.customersPage(w, r, ctx, id, space, http.StatusBadRequest, errKey(err))
}

func (d Deps) handleCustomersConfirm(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, err = verbund.ConfirmSuggestions(r.Context(), d.DB, ctx.Who, id, d.clientIP(r))
	d.customersBack(w, r, ctx, id, err)
}

// noneKey is the select's choice "no counterpart" (keys are ids, Ninja
// references or payer names, never this).
const noneKey = "!none"

// handleCustomerAct is what a cell does, for the customer "hub" of the
// leading member and the column "conn": link the select's party ("" forgets
// the link, noneKey says there is none), none, unlink, align the Kimai
// name to Ninja's; orphan forgets a stored customer without a leading one
// ("entry").
func (d Deps) handleCustomerAct(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	hub := r.FormValue("hub")
	conn, _ := strconv.ParseInt(r.FormValue("conn"), 10, 64)
	ip := d.clientIP(r)
	switch act, key := r.PathValue("act"), r.FormValue("party"); {
	case act == "link" && key == "":
		err = verbund.UnlinkCustomer(d.DB, ctx.Who, id, hub, conn, ip)
	case act == "link" && key == noneKey:
		err = verbund.LinkCustomer(r.Context(), d.DB, ctx.Who, id, hub, conn, "", ip)
	case act == "link":
		err = verbund.LinkCustomer(r.Context(), d.DB, ctx.Who, id, hub, conn, key, ip)
	case act == "none":
		err = verbund.LinkCustomer(r.Context(), d.DB, ctx.Who, id, hub, conn, "", ip)
	case act == "unlink":
		err = verbund.UnlinkCustomer(d.DB, ctx.Who, id, hub, conn, ip)
	case act == "align":
		err = verbund.AlignName(r.Context(), d.DB, ctx.Who, id, hub, ip)
	case act == "orphan":
		entry, _ := strconv.ParseInt(r.FormValue("entry"), 10, 64)
		err = verbund.RemoveOrphan(d.DB, ctx.Who, id, entry, ip)
	default:
		http.NotFound(w, r)
		return
	}
	d.customersBack(w, r, ctx, id, err)
}

// verbundPath is a space's Verbünde page.
func verbundPath(spaceID int64) string { return spacePath(spaceID) + "/verbund" }

func (d Deps) handleVerbundPage(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if !d.settingsOpen(w, r, ctx, id) {
		return
	}
	d.verbundPage(w, r, ctx, id, http.StatusOK, "")
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
	implicit, hasImplicit, err := verbund.Implicit(d.DB, ctx.Who, spaceID)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	values := map[string]any{"Verbuende": list, "Vague": vague, "Editable": ordered, "SpaceID": spaceID, "Error": errKeyText}
	if hasImplicit {
		values["Implicit"] = implicit
	}
	if err := d.levelValues(ctx, spaceID, values); err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	values[navPath] = verbundPath(spaceID)
	_ = d.Page(w, ctx, "verbund", status, values)
}

// handleVerbundSettle stores the space's implicit Verbund and opens its
// customers.
func (d Deps) handleVerbundSettle(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	space, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	id, err := verbund.Settle(d.DB, ctx.Who, space, d.clientIP(r))
	if err != nil {
		d.verbundRefused(w, r, ctx, space, err)
		return
	}
	http.Redirect(w, r, customersPath(id, space), http.StatusSeeOther)
}

// verbundBack returns to the space's page; a refusal shows the page
// again with the error and the forms as typed.
func (d Deps) verbundBack(w http.ResponseWriter, r *http.Request, ctx Ctx, err error) {
	space, _ := strconv.ParseInt(r.FormValue("space"), 10, 64)
	if err == nil {
		http.Redirect(w, r, verbundPath(space), http.StatusSeeOther)
		return
	}
	d.verbundRefused(w, r, ctx, space, err)
}

// verbundRefused answers a refused Verbund form: denied and unknown as
// such, anything else on the page with its error.
func (d Deps) verbundRefused(w http.ResponseWriter, r *http.Request, ctx Ctx, space int64, err error) {
	if isAny(err, deniedErrors) || isAny(err, notFoundErrors) || space == 0 {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	d.verbundPage(w, r, ctx, space, http.StatusBadRequest, errKey(err))
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
	d.verbundAnswer(w, r, ctx, err)
}

// verbundAnswer is verbundBack with a success message.
func (d Deps) verbundAnswer(w http.ResponseWriter, r *http.Request, ctx Ctx, err error) {
	if err == nil {
		d.flash(w, flashSaved)
	}
	d.verbundBack(w, r, ctx, err)
}

func (d Deps) handleVerbundRename(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d.verbundAnswer(w, r, ctx, verbund.Rename(d.DB, ctx.Who, id, r.FormValue("name"), d.clientIP(r)))
}

func (d Deps) handleVerbundAdd(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d.verbundBack(w, r, ctx, verbund.AddMember(d.DB, ctx.Who, id, formID(r, "conn"), d.clientIP(r)))
}

func (d Deps) handleVerbundRemove(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err1 := pathID(r, "id")
	conn, err2 := pathID(r, "conn")
	if err1 != nil || err2 != nil {
		http.NotFound(w, r)
		return
	}
	d.verbundBack(w, r, ctx, verbund.RemoveMember(d.DB, ctx.Who, id, conn, d.clientIP(r)))
}

func (d Deps) handleVerbundDelete(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	d.verbundBack(w, r, ctx, verbund.Delete(d.DB, ctx.Who, id, d.clientIP(r)))
}
