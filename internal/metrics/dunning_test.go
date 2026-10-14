package metrics_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// Invoice Ninja's schedule decides: the next enabled stage after those
// sent, from its date on; disabled stages are skipped.
func TestDunningCases(t *testing.T) {
	today := time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC)
	n := &sources.NinjaDataset{
		Reminders: [3]sources.NinjaReminder{
			{Enabled: true, Days: 3, Schedule: sources.AfterDueDate},
			{Enabled: false},
			{Enabled: true, Days: 30, Schedule: sources.AfterInvoiceDate},
		},
		Invoices: []sources.NinjaInvoice{
			{Key: "fresh", Status: "sent", Date: "2026-10-01", DueDate: "2026-10-12", Balance: 100},           // due + 3 not yet
			{Key: "first", Status: "sent", Date: "2026-10-01", DueDate: "2026-10-10", Balance: 100},           // → 1
			{Key: "third", Status: "sent", Date: "2026-09-01", DueDate: "2026-09-15", Balance: 100, Stage: 1}, // 2 off → 3
			{Key: "waits", Status: "sent", Date: "2026-09-20", DueDate: "2026-09-30", Balance: 100, Stage: 1}, // 3 on 20.10.
			{Key: "done", Status: "sent", Date: "2026-06-01", DueDate: "2026-06-15", Balance: 100, Stage: 3},
		},
	}
	got := map[string]int{}
	for _, c := range metrics.DunningCases(n, today) {
		got[c.Key] = c.Next
	}
	if len(got) != 2 || got["first"] != 1 || got["third"] != 3 {
		t.Errorf("cases: %v", got)
	}
	if metrics.DunningPlanOf(n).Defaults {
		t.Error("Ninja's plan read as defaults")
	}

	// Nothing set up in Invoice Ninja: Andon's defaults, said so.
	n.Reminders = [3]sources.NinjaReminder{}
	if p := metrics.DunningPlanOf(n); !p.Defaults || p.Reminders[1].Days != metrics.DunningFirst+metrics.DunningGap {
		t.Errorf("defaults: %+v", p)
	}
}
