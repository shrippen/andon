package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/services/widgetlib"
	"andon/internal/testkit"
	"andon/internal/widgets"
)

// TestEveryTileRendersFromDemo: each type renders from its gallery demo
// data without a template error or an inline style (CSP forbids it). A
// new type is covered without being listed anywhere.
func TestEveryTileRendersFromDemo(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "demo@x.y", enums.RoleAdmin)

	for _, kind := range widgets.AllTypes() {
		// Tiles fetching the internet (weather, apod) render their error
		// instead of waiting on the network.
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		frag, err := widgetlib.Demo(ctx, d, who, space, kind.Key, "", nil)
		cancel()
		if err != nil {
			t.Errorf("%s: demo: %v", kind.Key, err)
			continue
		}
		rec := httptest.NewRecorder()
		err = (Deps{}).Page(rec, Ctx{Locale: enums.LocaleDE, CSRF: "x"}, kind.Template, http.StatusOK,
			map[string]any{"ThemeURL": "", "Frag": frag, "Kind": kind})
		if err != nil {
			t.Errorf("%s: render: %v", kind.Key, err)
			continue
		}
		if strings.Contains(rec.Body.String(), ` style="`) {
			t.Errorf("%s: inline style attribute", kind.Key)
		}
	}
}
