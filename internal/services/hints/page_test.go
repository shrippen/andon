package hints

import "testing"

// TestPageOf: an action counts for the Andon page it leads to; another
// service's page counts for none.
func TestPageOf(t *testing.T) {
	cases := map[string]string{"/vat?span=2026-09#cases": "/vat", "/inventory#cases": "/inventory", "/billing": "/billing",
		"https://ninja.example/invoices/3": "", "//evil.example/x": "", "": ""}
	for in, want := range cases {
		got, ok := pageOf(in)
		if got != want || ok != (want != "") {
			t.Fatalf("%q: %q %v", in, got, ok)
		}
	}
}
