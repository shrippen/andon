package metrics

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// The balance of past days follows from today's balance and the bookings
// since: 1000 today after spending 200 yesterday was 1200 the day before.
func TestSureCashDays(t *testing.T) {
	today := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	data := &sources.SureDataset{
		Accounts: []sources.SureAccount{{Name: "Giro", Type: "depository", Balance: 1000}, {Name: "Depot", Type: "investment", Balance: 5000}},
		Transactions: []sources.SureTxn{
			{Date: "2026-09-27", Amount: -200, Account: "Giro"},
			{Date: "2026-09-27", Amount: 999, Account: "Depot"},
		},
	}
	days := SureCashDays(data, today, 3)
	if len(days) != 3 || days[2] != 1000 || days[1] != 1000 || days[0] != 1200 {
		t.Fatalf("days: %v", days)
	}
}
