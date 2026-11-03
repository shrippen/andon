package web

import (
	"bytes"
	"testing"

	"andon/internal/enums"
	"andon/internal/metrics"
)

// The suggestion line lists the picks for its field, comma-separated,
// a fixed name by its i18n key, and is absent without picks: the model only suggests.
func TestSuggestLine(t *testing.T) {
	set := newPageSet()
	set.state.locale = enums.LocaleDE
	render := func(picks []metrics.Pick) string {
		var b bytes.Buffer
		if err := set.tmpl.ExecuteTemplate(&b, "suggest", map[string]any{"For": "euer-1", "Picks": picks}); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}

	want := `<p class="field-suggest" data-suggest-for="euer-1"><span>Vorschlag:</span> ` +
		`<button type="button" class="field-suggest-pick" data-value="7">Arbeitsmittel</button>, ` +
		`<button type="button" class="field-suggest-pick" data-value="none">Vorschlag:</button></p>`
	if got := render([]metrics.Pick{{Value: "7", Label: "Arbeitsmittel"}, {Value: "none", Text: "learn.suggest"}}); got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if got := render(nil); got != "" {
		t.Fatalf("no picks: %q", got)
	}
}
