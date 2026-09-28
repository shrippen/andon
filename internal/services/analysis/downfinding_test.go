package analysis

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/model"
)

// A source error that is a catalog key ("truenas.https_required") reaches
// the reader as its text, not as the key; other errors stay as they are.
func TestDownFindingTranslatesKeys(t *testing.T) {
	conn := &model.Connection{ID: 1, Name: "Regis", Service: "truenas"}
	f := downFinding(conn, "truenas.https_required")
	got := i18n.T("hint.system.connector_down.why", enums.LocaleDE, i18n.Typed(f.Params, enums.LocaleDE))
	if want := i18n.T("truenas.https_required", enums.LocaleDE, nil); want == "truenas.https_required" || !contains(got, want) {
		t.Fatalf("why: %q", got)
	}
	plain := downFinding(conn, "HTTP 502")
	if plain.Params["error"] != "HTTP 502" {
		t.Fatalf("plain error: %+v", plain.Params)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
