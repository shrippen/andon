package web

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/services/boards"
	"andon/internal/services/widgetlib"
	"andon/internal/widgets"
)

// detailTemplate is the prefix of a type's dialog template:
// "details/link" draws the link tile's dialog.
const detailTemplate = "details/"

// detailBlocks draws any widgets.DetailBody (most types).
const detailBlocks = "details/body"

// detailItemParam picks a list entry: /details/7?item=nas.
const detailItemParam = "item"

// handleDetail answers a tile's detail dialog (opened by [data-details],
// see andon.js). Every type shares the head ("detail_head"); its template
// draws the body in one of Kante's layouts.
func (d Deps) handleDetail(w http.ResponseWriter, r *http.Request) {
	ctx, err := d.Viewer(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}

	item := r.URL.Query().Get(detailItemParam)
	dialog, err := boards.Detail(r.Context(), d.DB, ctx.Who, id, item)
	if errors.Is(err, widgetlib.ErrNoDetail) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	name := detailTemplate + dialog.Type
	if body, blocks := dialog.Body.(*widgets.DetailBody); blocks {
		name = detailBlocks
		for i, b := range tablesOf(body) {
			t := b.Data.(widgets.Table)
			t.CSV = fmt.Sprintf("/details/%d/csv/%d?%s=%s", id, i+1, detailItemParam, url.QueryEscape(item))
			b.Data = t
		}
	}
	_ = d.Page(w, ctx, name, http.StatusOK, map[string]any{"Dialog": dialog, "D": dialog.Body, "PlacementID": id, "ThemeURL": ""})
}

// tablesOf lists a body's table blocks in drawing order, pairs and tabs
// included: the nth is /details/{id}/csv/{n}.
func tablesOf(body *widgets.DetailBody) []*widgets.Block {
	var out []*widgets.Block
	var walk func(blocks []widgets.Block)
	walk = func(blocks []widgets.Block) {
		for i := range blocks {
			switch blocks[i].Data.(type) {
			case widgets.Table:
				out = append(out, &blocks[i])
			case []widgets.Block:
				walk(blocks[i].Data.([]widgets.Block))
			}
		}
	}
	walk(body.Blocks)
	for _, tab := range body.Tabs {
		walk(tab.Blocks)
	}
	return out
}

// handleDetailCSV answers one table of a dialog as CSV, values as the
// reader sees them.
func (d Deps) handleDetailCSV(w http.ResponseWriter, r *http.Request) {
	ctx, err := d.Viewer(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	dialog, err := boards.Detail(r.Context(), d.DB, ctx.Who, id, r.URL.Query().Get(detailItemParam))
	if errors.Is(err, widgetlib.ErrNoDetail) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	body, ok := dialog.Body.(*widgets.DetailBody)
	tables := []*widgets.Block{}
	if ok {
		tables = tablesOf(body)
	}
	if n < 1 || n > len(tables) {
		http.NotFound(w, r)
		return
	}
	blob, err := tableCSV(tables[n-1].Data.(widgets.Table), ctx.Locale)
	if err != nil {
		d.fail(w, err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="andon-%d-%d.csv"`, id, n))
	w.Write(blob)
}

// tableCSV writes a dialog table: head, rows, foot, values formatted as
// in the dialog.
func tableCSV(t widgets.Table, locale enums.Locale) ([]byte, error) {
	value := func(v any) string {
		if v == nil {
			return ""
		}
		return fmt.Sprint(i18n.Typed(map[string]any{"v": v}, locale)["v"])
	}
	var buf bytes.Buffer
	out := csv.NewWriter(&buf)
	head := make([]string, len(t.Head))
	for i, h := range t.Head {
		head[i] = i18n.T(h.Key, locale, i18n.Typed(h.Args, locale))
	}
	rows := append([][]widgets.Cell(nil), t.Rows...)
	if len(t.Foot) > 0 {
		rows = append(rows, t.Foot)
	}
	if err := out.Write(head); err != nil {
		return nil, err
	}
	for _, row := range rows {
		line := make([]string, len(row))
		for i, c := range row {
			line[i] = value(c.Value)
		}
		if err := out.Write(line); err != nil {
			return nil, err
		}
	}
	out.Flush()
	return buf.Bytes(), out.Error()
}
