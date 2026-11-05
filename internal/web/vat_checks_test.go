package web

import (
	"strings"
	"testing"

	"andon/internal/enums"
	"andon/internal/metrics"
)

// Every kind of check has its text on the page, with the amount it names.
func TestVATChecksRender(t *testing.T) {
	kinds := []metrics.CheckKind{metrics.CheckRate, metrics.CheckZero, metrics.CheckForeignTax, metrics.CheckNoCountry, metrics.CheckTaxOnTop}
	var e metrics.Elster
	for _, k := range kinds {
		e.Checks = append(e.Checks, metrics.ElsterCheck{Kind: k, Day: "2026-09-10", Name: "Acme", Number: "A1", Amount: 119, Diff: 3.61})
	}

	set := newPageSet()
	set.state.locale = enums.LocaleDE
	var b strings.Builder
	if err := set.tmpl.ExecuteTemplate(&b, "vat_checks", map[string]any{"E": e, "Currency": "EUR"}); err != nil {
		t.Fatal(err)
	}
	page := b.String()
	if strings.Count(page, "<li>") != len(kinds) || !strings.Contains(page, "3,61") {
		t.Fatalf("checks:\n%s", page)
	}
	if strings.Contains(page, "vatcheck.check") {
		t.Fatalf("a text is missing:\n%s", page)
	}
}
