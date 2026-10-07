package metrics

import (
	"testing"

	"andon/internal/sources"
)

// Every interval of a Sure recurring payment becomes a monthly amount,
// also the short and long ones that kept their raw amount (a weekly 10 €
// read as 10 € a month, a two-year 240 € as 240 € a month).
func TestMonthlyEveryInterval(t *testing.T) {
	cases := []struct {
		name       string
		last, next string
		amount     float64
		want       float64
	}{
		{"daily", "2026-09-01", "2026-09-02", 2, 60.83},
		{"weekly", "2026-09-01", "2026-09-08", 10, 43.33},
		{"fortnightly", "2026-09-01", "2026-09-15", 10, 21.67},
		{"monthly", "2026-08-01", "2026-09-01", 50, 50},
		{"february", "2026-02-01", "2026-03-01", 50, 50},
		{"quarterly", "2026-06-01", "2026-09-01", 30, 10},
		{"yearly", "2025-09-01", "2026-09-01", 120, 10},
		{"two years", "2024-09-01", "2026-09-01", 240, 10},
	}
	for _, c := range cases {
		got := round2(monthly(sources.SureRecurring{Amount: c.amount, Last: c.last, Next: c.next}))
		if got != c.want {
			t.Errorf("%s: %v a month, want %v", c.name, got, c.want)
		}
	}
}
