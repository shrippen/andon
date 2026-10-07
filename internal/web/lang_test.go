package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/services/widgetlib"
	"andon/internal/widgets"
)

// A dialog's hint list names the rule by its title, not its raw ID.
func TestDialogHintBlockNamesRule(t *testing.T) {
	body := &widgets.DetailBody{Blocks: []widgets.Block{{Kind: widgets.BlockHints,
		Data: []widgets.DetailHint{{Rule: "scrutiny.disk_hot", Severity: enums.SeverityCritical, Title: "hot", FirstSeen: time.Now()}}}}}
	dialog := &widgetlib.DetailDialog{Type: "disks", Body: body}

	rec := httptest.NewRecorder()
	if err := (Deps{}).Page(rec, Ctx{Locale: enums.LocaleEN}, detailBlocks, http.StatusOK, map[string]any{"Dialog": dialog, "D": body, "ThemeURL": ""}); err != nil {
		t.Fatal(err)
	}
	got := rec.Body.String()
	if strings.Contains(got, "scrutiny.disk_hot") {
		t.Errorf("raw rule ID in the hint list: %s", got)
	}
	if !strings.Contains(got, "Disk temperature") {
		t.Errorf("rule title missing: %s", got)
	}
}

// A dialog without a tile title takes the name the tile reads (TitleKey)
// before the generic type name.
func TestDialogTitleFromKey(t *testing.T) {
	dialog := &widgetlib.DetailDialog{Type: "kpi", Head: widgetlib.DetailHead{TitleKey: "opt.hours_today"}, Body: &widgets.DetailBody{}}

	rec := httptest.NewRecorder()
	if err := (Deps{}).Page(rec, Ctx{Locale: enums.LocaleEN}, detailBlocks, http.StatusOK, map[string]any{"Dialog": dialog, "D": dialog.Body, "ThemeURL": ""}); err != nil {
		t.Fatal(err)
	}
	got := rec.Body.String()
	if !strings.Contains(got, `<h3 id="detail-title">Hours today</h3>`) {
		t.Errorf("dialog title not from its key: %s", got)
	}
}
