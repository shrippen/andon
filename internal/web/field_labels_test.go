package web_test

import (
	"sort"
	"testing"

	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/widgets"
)

// TestEveryTileOptionHasText: each config field and select option of
// every tile type has a label in both languages, so the editor never
// shows a raw key.
func TestEveryTileOptionHasText(t *testing.T) {
	missing := map[string]bool{}
	check := func(key string) {
		for _, loc := range []enums.Locale{enums.LocaleDE, enums.LocaleEN} {
			if i18n.T(key, loc, nil) == key {
				missing[string(loc)+": "+key] = true
			}
		}
	}
	for _, kind := range widgets.AllTypes() {
		values := append(widgets.FormValues(kind.Key, nil), widgets.FrameFormValues(kind.Key, nil)...)
		for _, v := range values {
			check("field." + v.Label)
			for _, o := range v.Options {
				check("opt." + o)
			}
		}
	}
	keys := make([]string, 0, len(missing))
	for k := range missing {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Error(k)
	}
}
