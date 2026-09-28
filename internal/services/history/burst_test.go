package history

import (
	"testing"
	"time"
)

// Hints of one rule that came or went in the same minute read as one
// line: "54 × unused service resolved", not 54 lines.
func TestFoldBursts(t *testing.T) {
	at := time.Date(2026, 9, 28, 10, 0, 5, 0, time.UTC)
	entries := []Entry{
		{At: at, Kind: "resolved", Rule: "system.unused_service", Subject: "a", HintID: 1},
		{At: at.Add(-time.Second), Kind: "resolved", Rule: "system.unused_service", Subject: "b", HintID: 2},
		{At: at.Add(-2 * time.Second), Kind: "resolved", Rule: "system.unused_service", Subject: "c", HintID: 3},
		{At: at.Add(-2 * time.Second), Kind: "opened", Rule: "in.overdue", Subject: "x", HintID: 9},
		{At: at.Add(-3 * time.Second), Kind: "resolved", Rule: "system.unused_service", Subject: "d", HintID: 4},
		{At: at.Add(-time.Hour), Kind: "resolved", Rule: "system.unused_service", Subject: "e", HintID: 5},
	}
	got := foldBursts(entries, func(string) string { return "Ungenutzter Dienst" })
	if len(got) != 3 || got[0].Count != 4 || got[0].HintID != 0 || got[0].Subject != "Ungenutzter Dienst" || got[1].Count != 0 {
		t.Fatalf("folded: %+v", got)
	}
}
