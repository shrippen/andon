package web

// The gallery ("Kachel hinzufügen") is one catalog of every tile type
// (Vorlage), each card with a lazy preview and the count of tiles set
// up from it. A card opens a side panel with that type's tiles.
//
//	/widgets/new?dialog ──► gallery ──┬─ set up: /widgets/new?type=…&dialog (editor in the same dialog)
//	                                  └─ card ─► side: GET /widget-tiles/{type}
//	                                               ├─ existing: dialog ─┬─ show here too: POST …/place
//	                                               │                    └─ as a copy: POST /widgets/{id}/copy
//	                                               └─ delete unused: POST /widgets/unused/delete ─► gallery anew
//
// Previews: /widget-sample/{type} renders a type with the caller's
// connection (live) or demo data; /widgets/{id}/preview a library widget.

import (
	"andon/internal/services/icons"
	"cmp"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"

	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/services/access"
	"andon/internal/services/boards"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/services/widgetlib"
	"andon/internal/widgets"
)

// sideLimit caps the tiles the side panel lists; links run into the
// hundreds, a search narrows them.
const sideLimit = 50

// sideSearch is the number of tiles from which the side panel offers a search.
const sideSearch = 10

// galleryCard is one type to set up. ConnID is the caller's connection of
// its service (live preview), 0 means demo data. Tiles counts the tiles
// set up from it, Unused those on no board.
type galleryCard struct {
	Kind          widgets.WidgetType
	Name, Desc    string
	ConnID        int64
	Tiles, Unused int
}

// galleryTopic is one topic with its types, A–Z by displayed name.
type galleryTopic struct {
	Topic widgets.Topic
	Cards []galleryCard
}

// galleryTile is a set-up widget that can be placed again.
type galleryTile struct {
	widgetlib.Ref
	Name string
}

// galleryTarget names the section a tile goes to ("Start › Daten").
type galleryTarget struct {
	Board, Section string
	Size           enums.TileSize
}

// galleryClean is one type's unused tiles the caller may delete; IDs as
// one comma list ("4,9,12"), the way the cleanup form posts them.
type galleryClean struct {
	Name string
	IDs  string
	N    int
}

// galleryAfter is what the gallery shows after a deletion: the side
// panel of Open again, and how many tiles went.
type galleryAfter struct {
	Open    string
	Deleted int
	Done    bool
}

// connOf maps each service to the caller's first connection of it.
func (d Deps) connOf(ctx Ctx) (map[enums.ServiceType]int64, error) {
	conns, err := connections.Listing(d.DB, ctx.Who, enums.RightUse)
	if err != nil {
		return nil, err
	}
	out := map[enums.ServiceType]int64{}
	for _, c := range conns {
		if _, seen := out[c.Service]; !seen {
			out[c.Service] = c.ID
		}
	}
	return out, nil
}

func (d Deps) handleGallery(w http.ResponseWriter, ctx Ctx, target widgetTarget, spaces []access.SpaceRef, after galleryAfter) {
	connOf, err := d.connOf(ctx)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	lib, err := widgetlib.Library(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}

	// Per type: tiles, unused ones, and those the caller may delete.
	tiles := map[string]int{}
	unused := map[string]int{}
	deletable := map[string][]string{}
	for _, ref := range lib {
		tiles[ref.Type]++
		if ref.Uses > 0 {
			continue
		}
		unused[ref.Type]++
		if ref.CanDelete {
			deletable[ref.Type] = append(deletable[ref.Type], strconv.FormatInt(ref.ID, 10))
		}
	}

	sorter := collate.New(language.Make(string(ctx.Locale)), collate.IgnoreCase)
	tr := func(key string) string { return i18n.T(key, ctx.Locale, nil) }
	name := func(key string) string { return tr("wtype." + key + ".name") }
	byTopic := map[widgets.Topic][]galleryCard{}
	var clean []galleryClean
	for _, kind := range widgets.AllTypes() {
		card := galleryCard{Kind: kind, Name: name(kind.Key), Desc: tr("wtype." + kind.Key + ".desc"),
			ConnID: connOf[kind.Service], Tiles: tiles[kind.Key], Unused: unused[kind.Key]}
		topic := widgets.TopicOf(kind.Key)
		byTopic[topic] = append(byTopic[topic], card)
		if ids := deletable[kind.Key]; len(ids) > 0 {
			clean = append(clean, galleryClean{Name: card.Name, IDs: strings.Join(ids, ","), N: len(ids)})
		}
	}
	var topics []galleryTopic
	for _, topic := range widgets.Topics {
		cards := byTopic[topic]
		slices.SortFunc(cards, func(a, b galleryCard) int { return sorter.CompareString(a.Name, b.Name) })
		if len(cards) > 0 {
			topics = append(topics, galleryTopic{Topic: topic, Cards: cards})
		}
	}
	slices.SortFunc(clean, func(a, b galleryClean) int { return sorter.CompareString(a.Name, b.Name) })
	cleanN := 0
	for _, c := range clean {
		cleanN += c.N
	}

	var dest galleryTarget
	if target.Place {
		dest = d.targetNames(ctx, target)
	}
	if _, ok := widgets.Get(after.Open); !ok {
		after.Open = ""
	}

	_ = d.Page(w, ctx, "widget_gallery", http.StatusOK, map[string]any{"Partial": true, "ThemeURL": "",
		"Topics": topics, "Reuse": len(lib) > 0, "Target": target, "Dest": dest, "Spaces": spaces,
		"Clean": clean, "CleanN": cleanN, "After": after,
	})
}

// handleGalleryTiles renders the side panel of one type: its preview,
// "set up" and the tiles set up from it, at most sideLimit, ?q= narrows.
func (d Deps) handleGalleryTiles(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	kind, ok := widgets.Get(r.PathValue("type"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	connOf, err := d.connOf(ctx)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	lib, err := widgetlib.Library(d.DB, ctx.Who)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}

	typeName := i18n.T("wtype."+kind.Key+".name", ctx.Locale, nil)
	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	var tiles []galleryTile
	total, unused := 0, 0
	for _, ref := range lib {
		if ref.Type != kind.Key {
			continue
		}
		total++
		if ref.Uses == 0 {
			unused++
		}
		tile := galleryTile{Ref: ref, Name: cmp.Or(ref.Title, typeName)}
		if q != "" && !strings.Contains(strings.ToLower(tile.Name+" "+ref.Space.Name), q) {
			continue
		}
		tiles = append(tiles, tile)
	}
	sorter := collate.New(language.Make(string(ctx.Locale)), collate.IgnoreCase)
	slices.SortFunc(tiles, func(a, b galleryTile) int { return sorter.CompareString(a.Name, b.Name) })
	found := len(tiles)
	tiles = tiles[:min(found, sideLimit)]

	_ = d.Page(w, ctx, "gal_side", http.StatusOK, map[string]any{"Partial": true, "ThemeURL": "",
		"Kind": kind, "Name": typeName, "Desc": i18n.T("wtype."+kind.Key+".desc", ctx.Locale, nil),
		"ConnID": connOf[kind.Service], "Tiles": tiles, "Found": found, "Total": total, "Unused": unused,
		"Q": r.URL.Query().Get("q"), "Search": total > sideSearch, "Target": targetOf(r.URL.Query().Get),
	})
}

// handleUnusedDelete deletes the posted tiles that are on no board
// (ids: one or more comma lists) and shows the gallery anew.
func (d Deps) handleUnusedDelete(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var ids []int64
	for _, list := range r.Form["ids"] {
		for _, raw := range strings.Split(list, ",") {
			if id, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64); err == nil {
				ids = append(ids, id)
			}
		}
	}
	n, err := widgetlib.DeleteUnused(d.DB, ctx.Who, ids)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	d.handleGallery(w, ctx, targetOf(r.FormValue), access.EditableSpaces(ctx.Who),
		galleryAfter{Open: r.FormValue("open"), Deleted: n, Done: true})
}

// SetupURL is the address that sets up a tile of type for this target.
func (t widgetTarget) SetupURL(typeKey string) string {
	return "/widgets/new?type=" + typeKey + "&" + t.Query()
}

// Query is the target as query parameters for the side panel ("space=…").
func (t widgetTarget) Query() string {
	out := "space=" + strconv.FormatInt(t.SpaceID, 10)
	if !t.Place {
		return out
	}
	return out + "&section=" + strconv.FormatInt(t.SectionID, 10) + "&board=" + strconv.FormatInt(t.BoardID, 10) +
		"&version=" + strconv.Itoa(t.Version)
}

// targetNames looks up the board and section a new tile goes to; empty
// names if the board can't be read (the page still works without them).
func (d Deps) targetNames(ctx Ctx, target widgetTarget) galleryTarget {
	view, err := boards.View(d.DB, ctx.Who, target.BoardID, boards.LayoutOverlay)
	if err != nil {
		return galleryTarget{}
	}
	out := galleryTarget{Board: view.Name}
	for _, s := range view.Sections {
		if s.ID == target.SectionID {
			out.Section, out.Size = s.Title, s.Size
		}
	}
	return out
}

// handleSample renders a type with default settings: live with the given
// connection, else with demo data. ?rows=2 renders it as tall as a tile
// spanning two rows (more entries), e.g. for tools/tile-sizes.py.
func (d Deps) handleSample(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	var err error
	kind, ok := widgets.Get(r.PathValue("type"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	space, _ := strconv.ParseInt(r.URL.Query().Get("space"), 10, 64)
	rows, _ := strconv.Atoi(r.URL.Query().Get("rows"))
	config := widgets.ForRows(kind.Key, map[string]any{}, min(rows, boards.MaxTileRows))

	var frag *widgetlib.Fragment
	if conn, _ := strconv.ParseInt(r.URL.Query().Get("conn"), 10, 64); conn > 0 {
		frag, err = widgetlib.Preview(r.Context(), d.DB, ctx.Who, space, kind.Key, "", config, &conn)
	} else {
		frag, err = widgetlib.Demo(r.Context(), d.DB, ctx.Who, space, kind.Key, "", config)
	}
	d.renderPreview(w, ctx, kind, frag, err)
}

// handleWidgetShow renders a library widget as it looks on a board.
func (d Deps) handleWidgetShow(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	widget, _, err := widgetlib.Detail(d.DB, ctx.Who, id)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	kind, ok := widgets.Get(widget.Type)
	if !ok {
		http.NotFound(w, r)
		return
	}
	frag, err := widgetlib.Load(r.Context(), d.DB, ctx.Who, widget, svcdata.Cached)
	d.renderPreview(w, ctx, kind, frag, err)
}

// renderPreview writes a fragment into a preview card, or the error.
func (d Deps) renderPreview(w http.ResponseWriter, ctx Ctx, kind widgets.WidgetType, frag *widgetlib.Fragment, err error) {
	if err != nil {
		_ = d.Page(w, ctx, "widget_preview_error", http.StatusOK, map[string]any{"Error": errKey(err), "ThemeURL": ""})
		return
	}
	name := kind.Template
	values := map[string]any{"Frag": frag, "Kind": kind, "ThemeURL": ""}
	if kind.Key == linkType {
		name = "widget_preview"
		// The same icon the board shows: emoji, else an image.
		if link, ok := frag.Config.(widgets.LinkConfig); ok {
			icon := map[string]any{"Emoji": icons.Emoji(link.Icon), "Glyph": icons.Glyph(link.Icon)}
			if icon["Emoji"] == "" {
				icon["URL"] = icons.URL(icons.LinkSpec(link.Icon, frag.Title), link.URL)
			}
			values["Icon"] = icon
		}
	}
	_ = d.Page(w, ctx, name, http.StatusOK, values)
}
