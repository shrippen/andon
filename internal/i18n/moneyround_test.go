package i18n_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/i18n"
)

// TestMoneyRound: without cents, or in thousands for wall displays.
func TestMoneyRound(t *testing.T) {
	cases := []struct {
		mode   string
		locale enums.Notation
		want   string
	}{
		{"exact", enums.NotationDE, "109.449,31 €"},
		{"euro", enums.NotationDE, "109.449 €"},
		{"thousand", enums.NotationDE, "109,4 T€"},
		{"thousand", enums.NotationEN, "€109.4k"},
	}
	for _, c := range cases {
		if got := i18n.MoneyRound(109449.31, c.locale, "EUR", c.mode); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.mode, c.locale, got, c.want)
		}
	}
}
