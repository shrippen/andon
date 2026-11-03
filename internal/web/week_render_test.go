package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/services/week"
	"andon/internal/services/workbench"
	"andon/internal/sources"
)

// Learned projects and activities show as "Vorschlag:" under their
// select, each pick for its own select; nothing learned is selected.
func TestWeekSuggests(t *testing.T) {
	p := week.Page{Scopes: []workbench.Scope{{Key: "s"}}, Scope: "s",
		Catalog: &sources.KimaiCatalog{Projects: []sources.KimaiPick{{ID: 10, Name: "Kampagne"}, {ID: 11, Name: "Imagefilm"}},
			Activities: []sources.KimaiActivityPick{{ID: 5, Name: "Dreh"}}},
		Cases: []week.Case{{WeekGap: metrics.WeekGap{Key: "event:a", Kind: metrics.GapEvent}, Project: 10,
			ProjectPicks: []metrics.Pick{{Value: "11", Label: "Imagefilm"}}, ActivityPicks: []metrics.Pick{{Value: "5", Label: "Dreh"}}}}}
	rec := httptest.NewRecorder()
	if err := (Deps{}).Page(rec, Ctx{Locale: enums.LocaleDE}, "week", http.StatusOK, map[string]any{"ThemeURL": "", "P": p}); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`id="week-project-0"`, `<p class="field-suggest" data-suggest-for="week-project-0"><span>Vorschlag:</span> <button type="button" class="field-suggest-pick" data-value="11">Imagefilm</button></p>`,
		`id="week-activity-0"`, `<p class="field-suggest" data-suggest-for="week-activity-0"><span>Vorschlag:</span> <button type="button" class="field-suggest-pick" data-value="5">Dreh</button></p>`,
		`<option value="5">Dreh</option>`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("lacks %s:\n%s", want, body)
		}
	}
}
