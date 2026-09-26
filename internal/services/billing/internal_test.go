package billing

import (
	"testing"

	"andon/internal/metrics"
)

// TestBillableDrafts: internal work never becomes an invoice draft: no
// customer billed at rate 0, none on the space's internal list.
func TestBillableDrafts(t *testing.T) {
	drafts := []metrics.Draft{
		{Customer: "Nivre Film & Studio GmbH", Total: 5408.15},
		{Customer: "Intern", Total: 0},
		{Customer: "Vereinsarbeit", Total: 120},
		{Customer: "Saxonia", Total: 974.65},
	}
	settings := map[string]any{"billing": map[string]any{"internal": " vereinsarbeit , Sonstiges"}}
	var names []string
	for _, d := range billable(drafts, settings) {
		names = append(names, d.Customer)
	}
	if len(names) != 2 || names[0] != "Nivre Film & Studio GmbH" || names[1] != "Saxonia" {
		t.Fatalf("billable: %v", names)
	}
}
