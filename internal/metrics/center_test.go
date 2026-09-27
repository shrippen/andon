package metrics_test

import (
	"testing"
	"time"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// TestCenterOf: the space picks mean or median; mean without a choice.
func TestCenterOf(t *testing.T) {
	if c := metrics.CenterOf(nil); c != metrics.CenterMean {
		t.Fatalf("default %q", c)
	}
	median := map[string]any{"stats": map[string]any{"center": "median"}}
	if c := metrics.CenterOf(median); c != metrics.CenterMedian {
		t.Fatalf("median setting read as %q", c)
	}
	if got := metrics.CenterMedian.Of([]float64{9, 1, 100, 3}); got != 6 {
		t.Fatalf("median of 4 values: %v", got)
	}
	if got := metrics.CenterMean.Of([]float64{9, 1, 100, 3}); got != 28.25 {
		t.Fatalf("mean: %v", got)
	}
}

// TestPaymentDaysMedian: one invoice paid after 90 days makes a client
// who usually pays in 10 look slow by the mean, not by the median.
func TestPaymentDaysMedian(t *testing.T) {
	data := &sources.NinjaDataset{}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, wait := range []int{10, 10, 10, 90} {
		issued := start.AddDate(0, i*4, 0)
		data.Invoices = append(data.Invoices, sources.NinjaInvoice{ID: int64(i + 1), ClientID: 1, Status: "paid", Date: issued.Format(time.DateOnly), Amount: 100})
		data.Payments = append(data.Payments, sources.NinjaPayment{ID: int64(i + 1), ClientID: 1, Date: issued.AddDate(0, 0, wait).Format(time.DateOnly), Amount: 100})
	}
	if got := metrics.NinjaPaymentDays(data, metrics.CenterMean)[1]; got != 30 {
		t.Fatalf("mean days %d", got)
	}
	if got := metrics.NinjaPaymentDays(data, metrics.CenterMedian)[1]; got != 10 {
		t.Fatalf("median days %d", got)
	}
	rows := metrics.PaymentMorale(data, 5, metrics.CenterMedian)
	if len(rows) != 1 || rows[0].UsualDays != 10 {
		t.Fatalf("morale %+v", rows)
	}
}
