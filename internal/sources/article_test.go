package sources

import (
	"strings"
	"testing"
)

// The article's paragraphs win over a longer menu; short ones and
// scripts drop out.
func TestReadable(t *testing.T) {
	long := strings.Repeat("Förderung für Kurzfilme bis 30 Minuten. ", 3)
	page := `<html><body><nav><p>` + long + long + long + `</p></nav>
		<div><p>Kurz</p><p>` + long + `<script>x()</script></p><p>` + long + `</p></div></body></html>`
	got := readable(strings.NewReader(page))
	if len(got) != 2 || got[0] != strings.TrimSpace(long) {
		t.Fatalf("got %q", got)
	}
}
