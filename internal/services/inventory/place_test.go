package inventory

import (
	"errors"
	"testing"

	"andon/internal/sources"
)

// Kept's location comes from the option by name, else the first; a name
// not found is refused, not replaced by the first.
func TestKeptLocation(t *testing.T) {
	locs := []sources.KeptLocation{{ID: "a", Name: "Zuhause"}, {ID: "b", Name: "Studio Weber"}}
	cases := []struct {
		name, want string
		err        error
	}{
		{"", "a", nil},
		{"studio weber", "b", nil},
		{"Büro", "", errKeptLocation},
	}
	for _, c := range cases {
		got, err := keptLocation(locs, c.name)
		if got != c.want || !errors.Is(err, c.err) {
			t.Errorf("%q: %q %v", c.name, got, err)
		}
	}
	if _, err := keptLocation(nil, ""); !errors.Is(err, ErrNoWrite) {
		t.Errorf("none: %v", err)
	}
}

// YAML reads "place: 101" as a number; it is still the room's name.
func TestTextOption(t *testing.T) {
	opts := map[string]any{optPlace: 101, optLocation: " Studio "}
	if got := textOption(opts, optPlace); got != "101" {
		t.Errorf("number: %q", got)
	}
	if got := textOption(opts, optLocation); got != "Studio" {
		t.Errorf("text: %q", got)
	}
	if got := textOption(nil, optPlace); got != "" {
		t.Errorf("none: %q", got)
	}
}
