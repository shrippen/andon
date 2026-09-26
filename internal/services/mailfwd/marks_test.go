package mailfwd

import "testing"

// TestForwardedMark: a mail sent to Paperless is marked, and the mark
// alone is no reading of its invoice fields.
func TestForwardedMark(t *testing.T) {
	sent := map[string]any{forwardedKey: "2026-09-26T21:00:00Z"}
	read := map[string]any{"vendor": "Hetzner", forwardedKey: "2026-09-26T21:00:00Z"}
	if !isForwarded(sent) || hasRead(sent) {
		t.Fatalf("sent only: forwarded=%v read=%v", isForwarded(sent), hasRead(sent))
	}
	if !hasRead(read) || isForwarded(map[string]any{"vendor": "x"}) || isForwarded(nil) {
		t.Fatal("read fields or unsent mail misjudged")
	}
}
