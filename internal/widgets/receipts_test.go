package widgets_test

import (
	"strings"
	"testing"

	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// TestReceiptsMissing: business spending without receipt, largest first,
// each with a Paperless search; private accounts stay out.
func TestReceiptsMissing(t *testing.T) {
	kind, ok := widgets.Get("receipts_missing")
	if !ok {
		t.Fatal("receipts_missing not registered")
	}
	cfg, _ := widgets.Decode("receipts_missing", map[string]any{})
	sure := &sources.SureDataset{Currency: "EUR", Transactions: []sources.SureTxn{
		{ID: "1", Date: "2026-09-12", Name: "Hetzner", Amount: -38.2, Account: "Geschäft"},
		{ID: "2", Date: "2026-09-03", Name: "Thomann", Amount: -249, Account: "Geschäft"},
		{ID: "3", Date: "2026-09-05", Name: "Supermarkt", Amount: -80, Account: "Privat"},
	}}
	settings := map[string]any{"rules": map[string]any{"cross.expense_unrecorded": map[string]any{"accounts": []any{"geschäft"}}}}
	view := kind.View(cfg, map[string]any{"data": sure, "paperless": &sources.PaperlessDataset{URL: "https://pl.test"}},
		ctxFor(enums.ServiceSure, settings))
	rows := view["Rows"].([]widgets.ReceiptRow)
	if len(rows) != 2 || rows[0].Name != "Thomann" || rows[0].Amount != 249 || !strings.HasPrefix(rows[0].Search, "https://pl.test/documents?query=") {
		t.Fatalf("rows: %+v", rows)
	}
	if view["Sum"] != 287.2 {
		t.Fatalf("sum: %v", view["Sum"])
	}
}
