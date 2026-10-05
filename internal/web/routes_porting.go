package web

import (
	"errors"
	"net/http"
	"strconv"

	"andon/internal/services/access"
	"andon/internal/services/porting"
	"andon/internal/services/spaces"
)

const (
	yamlType      = "application/yaml; charset=utf-8"
	importDashy   = "dashy"
	maxImportSize = 2 << 20
)

// RegisterPortingRoutes wires YAML import/export: the import page (Dashy or
// our own format), a space's code view and the downloads.
func (d Deps) RegisterPortingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /import", d.authed(d.handleImportForm))
	mux.HandleFunc("POST /import", d.handleImportRun)
	mux.HandleFunc("GET /spaces/{id}/code", d.authed(d.handleSpaceCode))
	mux.HandleFunc("POST /spaces/{id}/code", d.authed(d.handleSpaceCodeSave))
	mux.HandleFunc("GET /spaces/{id}/export", d.handleSpaceExport)
	mux.HandleFunc("GET /boards/{id}/export", d.handleBoardExport)
}

func (d Deps) importPage(w http.ResponseWriter, ctx Ctx, status int, extra map[string]any) {
	values := map[string]any{"Spaces": access.EditableSpaces(ctx.Who)}
	for k, v := range extra {
		values[k] = v
	}
	_ = d.Page(w, ctx, "import", status, values)
}

func (d Deps) handleImportForm(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	d.importPage(w, ctx, http.StatusOK, nil)
}

// handleImportRun previews an upload first (a dry run that shows what
// would be created and skipped); the preview's confirm button sends the
// same text back with confirm set to import it.
func (d Deps) handleImportRun(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportSize+maxIconForm)
	if err := r.ParseMultipartForm(maxImportSize); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		http.Error(w, "import.too_large", http.StatusRequestEntityTooLarge)
		return
	}
	ctx, err := d.Require(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	space, err := strconv.ParseInt(r.FormValue("space_id"), 10, 64)
	if err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	text := r.FormValue("text")
	if blob, err := uploaded(r); err == nil {
		text = string(blob)
	}
	if text == "" {
		http.Error(w, "missing file", http.StatusBadRequest)
		return
	}

	kind := r.FormValue("kind")
	apply := porting.DryRun
	if r.FormValue("confirm") != "" {
		apply = porting.Commit
	}
	var report *porting.Report
	switch {
	case kind == importDashy && apply == porting.Commit:
		report, err = porting.ImportDashy(d.DB, ctx.Who, space, text)
	case kind == importDashy:
		report, err = porting.PreviewDashy(d.DB, ctx.Who, space, text)
	case apply == porting.Commit:
		report, err = porting.ImportSpace(d.DB, ctx.Who, space, text, porting.Merge)
	default:
		report, err = porting.PreviewSpace(d.DB, ctx.Who, space, text, porting.Merge)
	}
	if err != nil {
		d.importPage(w, ctx, http.StatusBadRequest, map[string]any{"Error": errKey(err)})
		return
	}
	if apply == porting.Commit {
		d.importPage(w, ctx, http.StatusOK, map[string]any{"Report": report})
		return
	}
	d.importPage(w, ctx, http.StatusOK, map[string]any{"Preview": report, "Text": text, "Kind": kind, "SpaceID": space})
}

func (d Deps) codePage(w http.ResponseWriter, ctx Ctx, space int64, status int, extra map[string]any) {
	values := map[string]any{"SpaceID": space}
	if _, ok := extra["Text"]; !ok {
		text, err := porting.ExportSpace(d.DB, ctx.Who, space)
		if err != nil {
			d.handleBoardError(w, nil, err)
			return
		}
		values["Text"] = text
	}
	for k, v := range extra {
		values[k] = v
	}
	_ = d.Page(w, ctx, "space_code", status, values)
}

func (d Deps) handleSpaceCode(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	space, err := pathID(r, "id")
	if err != nil || spaces.OpenSettings(d.DB, ctx.Who, space) != nil {
		http.NotFound(w, r)
		return
	}
	d.codePage(w, ctx, space, http.StatusOK, nil)
}

func (d Deps) handleSpaceCodeSave(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	space, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	text := r.FormValue("text")
	mode := porting.Mode(r.FormValue("mode"))
	if mode != porting.Merge {
		mode = porting.Replace
	}
	report, err := porting.ImportSpace(d.DB, ctx.Who, space, text, mode)
	if err != nil {
		d.codePage(w, ctx, space, http.StatusBadRequest, map[string]any{"Text": text, "Error": errKey(err)})
		return
	}
	d.codePage(w, ctx, space, http.StatusOK, map[string]any{"Report": report})
}

func (d Deps) sendYAML(w http.ResponseWriter, r *http.Request, name string, export func(Ctx, int64) (string, error)) {
	ctx, err := d.Require(r)
	if err != nil {
		d.handleAuthError(w, r, err)
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	text, err := export(ctx, id)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", yamlType)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+"-"+strconv.FormatInt(id, 10)+`.yml"`)
	w.Write([]byte(text))
}

func (d Deps) handleSpaceExport(w http.ResponseWriter, r *http.Request) {
	d.sendYAML(w, r, "space", func(ctx Ctx, id int64) (string, error) { return porting.ExportSpace(d.DB, ctx.Who, id) })
}

func (d Deps) handleBoardExport(w http.ResponseWriter, r *http.Request) {
	d.sendYAML(w, r, "board", func(ctx Ctx, id int64) (string, error) { return porting.ExportBoard(d.DB, ctx.Who, id) })
}
