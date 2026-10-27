package web

// A Verbund's categories, kept on its Invoice Ninja:
//
//	GET  /verbund/{id}/categories   Sure → Ninja category, Ninja category → EÜR line
//	POST /verbund/{id}/categories   stores both (sure:<name>, euer_slot, euer:<key>)
//	GET  /categories?ninja=3&bank=5 the same for connections in no Verbund
//	POST /categories

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"andon/internal/services/verbund"
)

// Field prefixes of the categories form.
const (
	catSure = "sure:"
)

func (d Deps) handleVerbundCategories(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	space, _ := strconv.ParseInt(r.URL.Query().Get("space"), 10, 64)
	values := map[string]any{"SpaceID": space, "Note": r.URL.Query().Get("note"), "Action": "/verbund/" + strconv.FormatInt(id, 10) + "/categories"}
	view, err := verbund.CategoriesOf(r.Context(), d.DB, ctx.Who, id)
	switch {
	case errors.Is(err, verbund.ErrNotFound):
		http.NotFound(w, r)
		return
	case err != nil:
		values["Error"] = errKey(err)
	}
	values["View"] = view
	if _, ok := ctx.Who.Spaces[space]; ok {
		if err := d.levelValues(ctx, space, values); err != nil {
			d.fail(w, err, http.StatusInternalServerError)
			return
		}
		values[navPath] = verbundPath(space)
	}
	_ = d.Page(w, ctx, "verbund_categories", http.StatusOK, values)
}

// categorySave reads the categories form.
func categorySave(r *http.Request) verbund.CategorySave {
	in := verbund.CategorySave{Sure: map[string]string{}}
	in.Slot, _ = strconv.Atoi(r.PostForm.Get("euer_slot"))
	for name, v := range r.PostForm {
		if strings.HasPrefix(name, catSure) {
			in.Sure[strings.TrimPrefix(name, catSure)] = v[0]
		}
	}
	return in
}

func (d Deps) handleVerbundCategoriesSave(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil || r.ParseForm() != nil {
		http.NotFound(w, r)
		return
	}
	in := categorySave(r)
	back := "/verbund/" + strconv.FormatInt(id, 10) + "/categories?space=" + r.FormValue("space")
	if err := verbund.SaveCategories(r.Context(), d.DB, ctx.Who, id, in, d.clientIP(r)); err != nil {
		http.Redirect(w, r, back+"&note="+errKey(err), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, back+"&note=verbund.categories_saved", http.StatusSeeOther)
}

// connCategories are the connections of the categories page outside a
// Verbund: ninja and bank (0 none).
func connCategories(r *http.Request) (ninja, bank int64, action string) {
	ninja, _ = strconv.ParseInt(r.FormValue("ninja"), 10, 64)
	bank, _ = strconv.ParseInt(r.FormValue("bank"), 10, 64)
	return ninja, bank, "/categories?ninja=" + strconv.FormatInt(ninja, 10) + "&bank=" + strconv.FormatInt(bank, 10)
}

func (d Deps) handleCategories(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	ninja, bank, action := connCategories(r)
	view, err := verbund.CategoriesFor(r.Context(), d.DB, ctx.Who, ninja, bank)
	values := map[string]any{"View": view, "Note": r.URL.Query().Get("note"), "Action": action}
	if err != nil {
		values["Error"] = errKey(err)
	}
	_ = d.Page(w, ctx, "verbund_categories", http.StatusOK, values)
}

func (d Deps) handleCategoriesSave(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	if r.ParseForm() != nil {
		http.NotFound(w, r)
		return
	}
	ninja, bank, action := connCategories(r)
	if err := verbund.SaveCategoriesFor(r.Context(), d.DB, ctx.Who, ninja, bank, categorySave(r), d.clientIP(r)); err != nil {
		http.Redirect(w, r, action+"&note="+errKey(err), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, action+"&note=verbund.categories_saved", http.StatusSeeOther)
}
