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
		{"Büro", "", ErrKeptLocation},
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
