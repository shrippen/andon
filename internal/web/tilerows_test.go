package web

import (
	"net/http/httptest"
	"testing"

	"andon/internal/services/boards"
)

// TestTileRowsOf: the height a tile's requests carry, kept within the
// grid's spans; without the header a tile is one row tall.
func TestTileRowsOf(t *testing.T) {
	cases := map[string]int{"": 1, "2": 2, "0": 1, "-3": 1, "99": boards.MaxTileRows, "x": 1}
	for header, want := range cases {
		r := httptest.NewRequest("GET", "/widget-fragments/1", nil)
		if header != "" {
			r.Header.Set(tileRowsHeader, header)
		}
		if got := tileRowsOf(r); got != want {
			t.Errorf("header %q: rows %d, want %d", header, got, want)
		}
	}
}
