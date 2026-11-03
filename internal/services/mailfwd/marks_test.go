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

// A new read replaces the old one; a field it lacks goes, the marks stay.
func TestReadReplaces(t *testing.T) {
	f := map[string]any{"vendor": "Alt", "number": "R1", dismissedKey: "x", forwardedKey: "y"}
	readInto(f, map[string]any{"vendor": "Neu"})
	if f["vendor"] != "Neu" || f["number"] != nil || f[dismissedKey] != "x" || f[forwardedKey] != "y" {
		t.Fatalf("after a new read: %v", f)
	}
}
