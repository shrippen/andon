package web

// A Verbund's categories, kept on its Invoice Ninja:
//
//	GET  /verbund/{id}/categories   Sure → Ninja category, Ninja category → EÜR line
//	POST /verbund/{id}/categories   stores both (sure:<name>, euer_slot, euer:<key>)

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
	catEuer = "euer:"
)

func (d Deps) handleVerbundCategories(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	space, _ := strconv.ParseInt(r.URL.Query().Get("space"), 10, 64)
	values := map[string]any{"SpaceID": space, "Note": r.URL.Query().Get("note")}
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

func (d Deps) handleVerbundCategoriesSave(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil || r.ParseForm() != nil {
		http.NotFound(w, r)
		return
	}
	in := verbund.CategorySave{Sure: map[string]string{}, Euer: map[string]string{}}
	in.Slot, _ = strconv.Atoi(r.PostForm.Get("euer_slot"))
	for name, v := range r.PostForm {
		switch {
		case strings.HasPrefix(name, catSure):
			in.Sure[strings.TrimPrefix(name, catSure)] = v[0]
		case strings.HasPrefix(name, catEuer):
			in.Euer[strings.TrimPrefix(name, catEuer)] = v[0]
		}
	}
	back := "/verbund/" + strconv.FormatInt(id, 10) + "/categories?space=" + r.FormValue("space")
	if err := verbund.SaveCategories(r.Context(), d.DB, ctx.Who, id, in, d.clientIP(r)); err != nil {
		http.Redirect(w, r, back+"&note="+errKey(err), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, back+"&note=verbund.categories_saved", http.StatusSeeOther)
}
