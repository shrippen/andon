package web

// A Verbund's accounts: the same bank account across Invoice Ninja,
// Sure and Firefly, and what each account is for.
//
//	GET  /verbund/{id}/accounts           the table
//	POST /verbund/{id}/accounts/confirm   stores every suggestion
//	POST /verbund/{id}/accounts/link      a cell: hub, conn, account
//	POST /verbund/{id}/accounts/role      an account's role: conn, key, role

import (
	"errors"
	"net/http"
	"strconv"

	"andon/internal/services/verbund"
)

func accountsPath(id, spaceID int64) string {
	return "/verbund/" + strconv.FormatInt(id, 10) + "/accounts?space=" + strconv.FormatInt(spaceID, 10)
}

func (d Deps) handleVerbundAccounts(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	space, _ := strconv.ParseInt(r.URL.Query().Get("space"), 10, 64)
	d.accountsPage(w, r, ctx, id, space, http.StatusOK, "")
}

// accountsPage renders a Verbund's accounts, with an error if one came.
func (d Deps) accountsPage(w http.ResponseWriter, r *http.Request, ctx Ctx, id, space int64, status int, errKeyText string) {
	values := map[string]any{"SpaceID": space, "Error": errKeyText}
	view, err := verbund.Accounts(r.Context(), d.DB, ctx.Who, id)
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
	_ = d.Page(w, ctx, "verbund_accounts", status, values)
}

// accountsBack returns to the accounts page, or shows it with the error.
func (d Deps) accountsBack(w http.ResponseWriter, r *http.Request, ctx Ctx, id int64, err error) {
	space, _ := strconv.ParseInt(r.FormValue("space"), 10, 64)
	if err == nil {
		http.Redirect(w, r, accountsPath(id, space), http.StatusSeeOther)
		return
	}
	if isAny(err, deniedErrors) || isAny(err, notFoundErrors) {
		d.fail(w, err, http.StatusBadRequest)
		return
	}
	d.accountsPage(w, r, ctx, id, space, http.StatusBadRequest, errKey(err))
}

func (d Deps) handleAccountsConfirm(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	_, err = verbund.ConfirmAccounts(r.Context(), d.DB, ctx.Who, id, d.clientIP(r))
	d.accountsBack(w, r, ctx, id, err)
}

// handleAccountAct links a cell ("" forgets, noneKey says there is none)
// or sets an account's role.
func (d Deps) handleAccountAct(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	conn, _ := strconv.ParseInt(r.FormValue("conn"), 10, 64)
	hub, ip := r.FormValue("hub"), d.clientIP(r)
	switch act, key := r.PathValue("act"), r.FormValue("account"); {
	case act == "link" && key == "":
		err = verbund.UnlinkAccount(d.DB, ctx.Who, id, hub, conn, ip)
	case act == "link" && key == noneKey:
		err = verbund.LinkAccount(r.Context(), d.DB, ctx.Who, id, hub, conn, "", ip)
	case act == "link":
		err = verbund.LinkAccount(r.Context(), d.DB, ctx.Who, id, hub, conn, key, ip)
	case act == "role":
		err = verbund.SetRole(d.DB, ctx.Who, conn, r.FormValue("key"), verbund.Role(r.FormValue("role")), ip)
	default:
		http.NotFound(w, r)
		return
	}
	d.accountsBack(w, r, ctx, id, err)
}
