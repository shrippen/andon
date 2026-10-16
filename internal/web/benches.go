package web

// The workbenches and evaluation pages behind the menu "Werkbänke": the
// overview /benches presents them grouped, the bar shows only the
// viewer's favourites, each with its open hints.
//
//	bar:   Boards · Hinweise · [Belege 7] [USt] · Werkbänke ▾
//	menu:  per group every bench with its count, then "Alle Werkbänke"
//
// A count is the open hints whose action leads to the bench
// (hints.Tally.Pages). Until the viewer stars one, the favourites are the
// benches with open hints.

import (
	"net/http"
	"slices"

	"andon/internal/services/access"
	"andon/internal/services/hints"
	"andon/internal/services/workbench"
)

// benchGroup is a heading of the menu and the overview.
type benchGroup string

const (
	groupMoney  benchGroup = "money"  // customers, hours, invoices, receipts, tax
	groupStock  benchGroup = "stock"  // devices and what runs on them
	groupReview benchGroup = "review" // the year and what changed
)

var benchGroups = []benchGroup{groupMoney, groupStock, groupReview}

// bench is one page of the menu; its label is "nav.<id>", its text
// "benches.desc.<id>".
type bench struct {
	id, url string
	group   benchGroup
}

var benches = []bench{
	{"clients", "/clients", groupMoney}, {"week", "/week", groupMoney}, {"billing", "/billing", groupMoney},
	{"receipts", "/receipts", groupMoney}, {"vat", "/vat", groupMoney}, {"euer", "/euer", groupMoney}, {"subs", "/subs", groupMoney},
	{"inventory", "/inventory", groupStock}, {"purchases", "/purchases", groupStock},
	{"hosts", "/hosts", groupStock}, {"updates", "/updates", groupStock},
	{"year", "/year", groupReview}, {"timeline", "/timeline", groupReview},
}

const benchesURL = "/benches"

// BenchItem is a bench as the menu and the overview show it.
type BenchItem struct {
	ID, URL string
	N       int // open hints
	Level   int // their highest severity
	Fav     bool
}

// BenchGroup is a heading with its benches.
type BenchGroup struct {
	Key   string
	Items []BenchItem
}

// benchMenu shapes the benches for the viewer: grouped, and the
// favourites in the bar's order.
func benchMenu(t hints.Tally, f workbench.Favs) (groups []BenchGroup, favs []BenchItem) {
	byGroup := map[benchGroup][]BenchItem{}
	for _, b := range benches {
		c := t.Pages[b.url]
		item := BenchItem{ID: b.id, URL: b.url, N: c.N, Level: int(c.Level)}
		item.Fav = c.N > 0
		if f.Chosen {
			item.Fav = slices.Contains(f.IDs, b.id)
		}
		byGroup[b.group] = append(byGroup[b.group], item)
		if item.Fav {
			favs = append(favs, item)
		}
	}
	for _, g := range benchGroups {
		groups = append(groups, BenchGroup{Key: string(g), Items: byGroup[g]})
	}
	return groups, favs
}

// favIDs are the ids of the benches the bar shows.
func favIDs(favs []BenchItem) []string {
	out := make([]string, len(favs))
	for i, b := range favs {
		out[i] = b.ID
	}
	return out
}

func (d Deps) RegisterBenchRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET "+benchesURL, d.authed(d.handleBenches))
	mux.HandleFunc("POST "+benchesURL+"/{id}/fav", d.authed(d.handleBenchFav))
}

// handleBenches is the overview; the header's menu data fills it.
func (d Deps) handleBenches(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	_ = d.Page(w, ctx, "benches", http.StatusOK, nil)
}

// handleBenchFav stars or unstars a bench, then back where it was.
func (d Deps) handleBenchFav(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id := r.PathValue("id")
	if !slices.ContainsFunc(benches, func(b bench) bool { return b.id == id }) {
		http.NotFound(w, r)
		return
	}
	_, favs, err := d.benchState(ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	if err := workbench.ToggleFav(d.DB, ctx.Who, id, favIDs(favs)); err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, backTo(r, benchesURL), http.StatusSeeOther)
}

// benchState is the viewer's menu from their open hints and stars.
func (d Deps) benchState(who *access.Principal) ([]BenchGroup, []BenchItem, error) {
	t, err := hints.Count(d.DB, who)
	if err != nil {
		return nil, nil, err
	}
	f, err := workbench.LoadFavs(d.DB, who)
	if err != nil {
		return nil, nil, err
	}
	groups, favs := benchMenu(t, f)
	return groups, favs, nil
}
