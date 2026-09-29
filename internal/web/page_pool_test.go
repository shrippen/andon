package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"andon/internal/enums"
	"andon/internal/services/widgetlib"
	"andon/internal/widgets"
)

// TestPagesRenderInParallel: pooled template sets serve concurrent
// requests in both languages without mixing their helpers.
func TestPagesRenderInParallel(t *testing.T) {
	cfg, _ := widgets.Decode("clock", nil)
	frag := &widgetlib.Fragment{Type: "clock", Config: cfg}
	var wg sync.WaitGroup
	for i := range 32 {
		locale, want := enums.LocaleDE, "Europe/Berlin"
		if i%2 == 1 {
			locale = enums.LocaleEN
		}
		wg.Go(func() {
			rec := httptest.NewRecorder()
			err := (Deps{}).Page(rec, Ctx{Locale: locale}, "widgets/clock", http.StatusOK, map[string]any{"ThemeURL": "", "Frag": frag})
			if err != nil || !strings.Contains(rec.Body.String(), want) {
				t.Errorf("%s: %v", locale, err)
			}
		})
	}
	wg.Wait()
}
