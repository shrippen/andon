package sources

import "testing"

// Bank texts pad names with spaces and cut them off with "..";
// "Co." and single spaces stay.
func TestCleanName(t *testing.T) {
	for in, want := range map[string]string{
		"DB Vertrieb GmbH                                  ..": "DB Vertrieb GmbH",
		"  Hetzner   Online  ":                                 "Hetzner Online",
		"Smith & Co.":                                          "Smith & Co.",
		"":                                                     "",
	} {
		if got := cleanName(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
