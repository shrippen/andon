package sources_test

import (
	"testing"

	"andon/internal/sources"
)

// TestBankMap: a booking's category decides, else its first mapped tag;
// both survive the connection's options.
func TestBankMap(t *testing.T) {
	c := sources.Categories{Sure: map[int64]map[string]string{5: {"Software": "lic"}},
		Tags: map[int64]map[string]string{5: {"Studio": "gear", "Reise": "travel"}}}
	options := map[string]any{}
	c.Options(options)
	m := sources.CategoriesOf(options).Of(5)

	cases := []struct {
		category string
		tags     []string
		want     string
	}{
		{"Software", []string{"Studio"}, "lic"},
		{"Unbekannt", []string{"Privat", "Studio", "Reise"}, "gear"},
		{"", nil, ""},
	}
	for _, tc := range cases {
		if got := m.Key(tc.category, tc.tags); got != tc.want {
			t.Errorf("%q %v: %q, want %q", tc.category, tc.tags, got, tc.want)
		}
	}
	if got := sources.CategoriesOf(options).Of(6).Key("Software", []string{"Studio"}); got != "" {
		t.Errorf("other bank: %q", got)
	}
}
