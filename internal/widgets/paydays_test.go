package widgets_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// TestPaymentDaysTile: one line per client with a dot per paid invoice,
// the typical value marked and the target on the same scale.
func TestPaymentDaysTile(t *testing.T) {
	kind, ok := widgets.Get("payment_days")
	if !ok {
		t.Fatal("payment_days not registered")
	}
	cfg, _ := widgets.Decode("payment_days", map[string]any{})
	data := &sources.NinjaDataset{Clients: []sources.NinjaClient{{ID: 1, Name: "Muster"}}}
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, wait := range []int{10, 10, 10, 90} {
		issued := start.AddDate(0, i*2, 0)
		data.Invoices = append(data.Invoices, sources.NinjaInvoice{ID: int64(i + 1), ClientID: 1, Status: "paid", Date: issued.Format(time.DateOnly), Amount: 100})
		data.Payments = append(data.Payments, sources.NinjaPayment{ID: int64(i + 1), ClientID: 1, Date: issued.AddDate(0, 0, wait).Format(time.DateOnly), Amount: 100})
	}
	median := map[string]any{"stats": map[string]any{"center": "median"}}
	view := kind.View(cfg, map[string]any{"data": data}, ctxFor(enums.ServiceInvoiceNinja, median))
	rows := view["Rows"].([]widgets.PayRow)
	if len(rows) != 1 || rows[0].Client != "Muster" || rows[0].Typical != 10 || len(rows[0].Dots) != 4 {
		t.Fatalf("rows: %+v", rows)
	}
	r := rows[0]
	if r.TypicalX >= r.TargetX || r.Dots[3] <= r.TargetX || r.TargetX <= 0 {
		t.Fatalf("scale: %+v", r)
	}
}
