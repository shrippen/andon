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

// A learned proposal says so; the customer's latest entry does not.
func TestWeekLearnedOrigin(t *testing.T) {
	render := func(from metrics.WeekFrom) string {
		p := week.Page{Scopes: []workbench.Scope{{Key: "s"}}, Scope: "s",
			Catalog: &sources.KimaiCatalog{Projects: []sources.KimaiPick{{ID: 10, Name: "Kampagne"}}, Activities: []sources.KimaiActivityPick{{ID: 5, Name: "Dreh"}}},
			Cases:   []week.Case{{WeekGap: metrics.WeekGap{Key: "event:a", Kind: metrics.GapEvent}, Project: 10, Activity: 5, From: from}}}
		rec := httptest.NewRecorder()
		if err := (Deps{}).Page(rec, Ctx{Locale: enums.LocaleDE}, "week", http.StatusOK, map[string]any{"ThemeURL": "", "P": p}); err != nil {
			t.Fatalf("render %s: %v", from, err)
		}
		return rec.Body.String()
	}
	const origin = `<span class="field-origin">gelernt</span>`
	if body := render(metrics.WeekFromLearned); !strings.Contains(body, origin) {
		t.Fatalf("learned without origin:\n%s", body)
	}
	if body := render(metrics.WeekFromLatest); strings.Contains(body, origin) {
		t.Fatalf("latest marked learned:\n%s", body)
	}
}
