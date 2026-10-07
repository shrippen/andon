package i18n

import (
	"regexp"
	"sort"
	"strings"
	"testing"

	"andon/internal/enums"
)

// germanInEnglish finds German in an English text: umlauts and ß, and
// words that are German only (not "die", "Tag" or "also").
var germanInEnglish = regexp.MustCompile(`[äöüßÄÖÜ]|\b(und|oder|nicht|für|mit|der|das|ist|sind|wird|werden|bei|auf|aus|zum|zur|vom|seit|noch|schon|auch|nur|alle|jede|eigene|neue|keine|kein|heute|gestern|offen|erledigt|Instanz|Verbund|Verbünde|Kennzahl|Bereich|Einstellungen|Hinweis|Hinweise|Kunde|Kunden|Rechnung|Speichern|Löschen|Abbrechen|Verbindung|Verbindungen|Kachel|Kacheln|Zeitraum|Monat|Jahr|Woche|Fehler|Betrag)\b`)

// germanAllowed are German names English texts must keep: services,
// demo companies and the labels of other programs' own German UIs.
var germanAllowed = []string{
	"Studio Weber",
	"Kintsugi → Einstellungen", // Kintsugi's settings page, as its UI names it
	`"Kündigungsfrist"`,        // Paperless custom fields Andon reads (sources/contracts.go)
	`"Vertragsende"`,
}

// TestEnglishHasNoGerman: an English text with a German word ("Verbund",
// "Instanz") is a translation that was forgotten.
func TestEnglishHasNoGerman(t *testing.T) {
	ensureLoaded()
	var found []string
	for key, text := range catalogs[enums.LocaleEN] {
		for _, name := range germanAllowed {
			text = strings.ReplaceAll(text, name, "")
		}
		if m := germanInEnglish.FindString(text); m != "" {
			found = append(found, key+" ("+m+")")
		}
	}
	sort.Strings(found)
	if len(found) > 0 {
		t.Errorf("German in en.yml: %v", found)
	}
}

// TestCatalogsHaveText: a key with text in one language and none in the
// other shows nothing to the readers of that one. Empty in both is a
// deliberate blank (energy.level.none).
func TestCatalogsHaveText(t *testing.T) {
	ensureLoaded()
	de, en := catalogs[enums.LocaleDE], catalogs[enums.LocaleEN]
	var lopsided []string
	for key, text := range de {
		if (strings.TrimSpace(text) == "") != (strings.TrimSpace(en[key]) == "") {
			lopsided = append(lopsided, key)
		}
	}
	sort.Strings(lopsided)
	if len(lopsided) > 0 {
		t.Errorf("text in one language only: %v", lopsided)
	}
}
