package web

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"

	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/services/boards"
	"andon/internal/services/detailacts"
	"andon/internal/services/mailfwd"
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
	d.renderDetail(w, r, ctx, id, r.URL.Query().Get(detailItemParam))
}

// renderDetail draws a placement's dialog with an entry picked.
func (d Deps) renderDetail(w http.ResponseWriter, r *http.Request, ctx Ctx, id int64, item string) {
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
		openItems(body, id, item)
		postTasks(body, id, item)
		fileFrames(body, id)
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

// openItems gives every entry with an Item its dialog URL, in the list
// and in status and row blocks, and marks the one shown.
func openItems(body *widgets.DetailBody, id int64, picked string) {
	link := func(rows []widgets.LitRow) {
		for i := range rows {
			if rows[i].Item == "" {
				continue
			}
			rows[i].Open = fmt.Sprintf("/details/%d?%s=%s", id, detailItemParam, url.QueryEscape(rows[i].Item))
			rows[i].Picked = rows[i].Item == picked
		}
	}
	if body.List != nil {
		link(body.List.Items)
	}
	var walk func(blocks []widgets.Block)
	walk = func(blocks []widgets.Block) {
		for _, b := range blocks {
			switch data := b.Data.(type) {
			case []widgets.LitRow:
				link(data)
			case []widgets.Block:
				walk(data)
			}
		}
	}
	walk(body.Blocks)
	for _, tab := range body.Tabs {
		walk(tab.Blocks)
	}
}

// postTasks gives every task with a Do its POST address: the act, its
// fields and the entry shown, so the dialog comes back as it was.
func postTasks(body *widgets.DetailBody, id int64, item string) {
	var walk func(blocks []widgets.Block)
	walk = func(blocks []widgets.Block) {
		for i := range blocks {
			switch data := blocks[i].Data.(type) {
			case widgets.Tasks:
				for j := range data.Items {
					t := &data.Items[j]
					if t.Do == "" {
						continue
					}
					q := url.Values{detailItemParam: {item}}
					for k, v := range t.Args {
						q.Set(k, v)
					}
					t.Post = fmt.Sprintf("/details/%d/do/%s?%s", id, url.PathEscape(t.Do), q.Encode())
				}
			case []widgets.Block:
				walk(data)
			}
		}
	}
	walk(body.Blocks)
	for _, tab := range body.Tabs {
		walk(tab.Blocks)
	}
}

// handleDetailDo runs an act a dialog offers and answers with the dialog
// drawn anew.
func (d Deps) handleDetailDo(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	err = detailacts.Run(r.Context(), d.DB, ctx.Who, id, r.PathValue("act"), r.Form, d.clientIP(r))
	switch {
	case errors.Is(err, detailacts.ErrUnknownAct), errors.Is(err, detailacts.ErrBadMark):
		http.Error(w, "bad act", http.StatusBadRequest)
		return
	case err != nil:
		d.handleBoardError(w, r, err)
		return
	}
	d.renderDetail(w, r, ctx, id, r.Form.Get(detailItemParam))
}

// fileFrames points embeds of a dialog file at /details/{id}/file.
func fileFrames(body *widgets.DetailBody, id int64) {
	var walk func(blocks []widgets.Block)
	walk = func(blocks []widgets.Block) {
		for i := range blocks {
			switch data := blocks[i].Data.(type) {
			case widgets.Embed:
				if data.File != "" {
					data.URL = fmt.Sprintf("/details/%d/file?%s", id, data.File)
					blocks[i].Data = data
				}
			case []widgets.Block:
				walk(data)
			}
		}
	}
	walk(body.Blocks)
	for _, tab := range body.Tabs {
		walk(tab.Blocks)
	}
}

// fileTypes are the attachment types a dialog shows inline.
var fileTypes = map[string]string{".pdf": "application/pdf", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp"}

// handleDetailFile answers an attachment of a dialog's mail for the
// dialog to show inline.
func (d Deps) handleDetailFile(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	uid, err1 := strconv.ParseUint(r.URL.Query().Get("uid"), 10, 32)
	n, err2 := strconv.Atoi(r.URL.Query().Get("n"))
	if err1 != nil || err2 != nil {
		http.NotFound(w, r)
		return
	}
	file, err := mailfwd.File(r.Context(), d.DB, ctx.Who, id, uint32(uid), n)
	if errors.Is(err, mailfwd.ErrNoFile) || errors.Is(err, mailfwd.ErrNoFiles) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	kind, ok := fileTypes[strings.ToLower(filepath.Ext(file.Name))]
	if !ok {
		http.Error(w, "unsupported", http.StatusUnsupportedMediaType)
		return
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("inline", map[string]string{"filename": file.Name}))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Write(file.Content)
}
