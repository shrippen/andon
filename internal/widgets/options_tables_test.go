package widgets_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// TestTableOptions: a column hidden by its shown name, rows sorted by
// amount, a sum row over money columns.
func TestTableOptions(t *testing.T) {
	ninja := &sources.NinjaDataset{Currency: "EUR", Clients: []sources.NinjaClient{{ID: 1, Name: "A"}, {ID: 2, Name: "B"}},
		Invoices: []sources.NinjaInvoice{
			{ID: 1, Number: "R1", ClientID: 1, Status: "sent", Date: "2026-09-01", DueDate: "2026-09-10", Balance: 100},
			{ID: 2, Number: "R2", ClientID: 2, Status: "sent", Date: "2026-09-02", DueDate: "2026-09-11", Balance: 300}}}
	v := viewOf(t, "table", map[string]any{"table": "open_invoices", "hide_cols": []any{"Fällig"}, "sort": "amount_desc", "sum_row": true},
		map[string]any{"data": ninja}, enums.ServiceInvoiceNinja, nil)
	cols := v["Cols"].([]widgets.Col)
	for _, c := range cols {
		if c.Label == "due" {
			t.Fatalf("hidden column shown: %+v", cols)
		}
	}
	rows := v["Rows"].([]widgets.Row)
	amount := len(cols) - 1
	if rows[0].Values[amount] != 300.0 {
		t.Fatalf("not sorted by amount: %+v", rows)
	}
	sum, _ := v["Sum"].(widgets.Row)
	if len(sum.Values) != len(cols) || sum.Values[amount] != 400.0 {
		t.Fatalf("sum row: %+v", v["Sum"])
	}
}

// TestAgingOptions: own band limits and clients left out.
func TestAgingOptions(t *testing.T) {
	ninja := &sources.NinjaDataset{Clients: []sources.NinjaClient{{ID: 1, Name: "Verein"}, {ID: 2, Name: "Kunde"}},
		Invoices: []sources.NinjaInvoice{
			{ID: 1, ClientID: 1, Status: "sent", Date: "2026-08-01", DueDate: "2026-08-10", Balance: 100},
			{ID: 2, ClientID: 2, Status: "sent", Date: "2026-08-01", DueDate: "2026-09-05", Balance: 50}}}
	v := viewOf(t, "invoice_aging", map[string]any{"bands": []any{7.0, 14.0}, "hide_clients": []any{"verein"}}, map[string]any{"data": ninja}, enums.ServiceInvoiceNinja, nil)
	if v["Total"] != 50.0 {
		t.Fatalf("client not hidden: %+v", v)
	}
	bands := v["Bands"].([]widgets.AgingBand)
	if bands[2].Amount != 50 { // 10 days late: past 7, within 14
		t.Fatalf("bands: %+v", bands)
	}
}

// TestUnbilledOptions: own limits, internal customers from the space
// settings left out.
func TestUnbilledOptions(t *testing.T) {
	kimai := &sources.KimaiDataset{Customers: []sources.KimaiCustomer{{ID: 1, Name: "Verein"}, {ID: 2, Name: "Kunde"}},
		Timesheets: []sources.KimaiSheet{
			{CustomerID: 1, Begin: "2026-09-01", End: "2026-09-01", Billable: true, Rate: 100},
			{CustomerID: 2, Begin: "2026-09-10", End: "2026-09-10", Billable: true, Rate: 80}}}
	settings := map[string]any{"billing": map[string]any{"internal": "Verein"}}
	v := viewOf(t, "unbilled_age", map[string]any{"bands": []any{3.0, 4.0}, "hide_internal": true}, map[string]any{"data": kimai}, enums.ServiceKimai, settings)
	if v["Total"] != 80.0 {
		t.Fatalf("internal customer counted: %+v", v)
	}
	bands := v["Bands"].([]widgets.AgingBand)
	if bands[2].Amount != 80 { // 5 days old: past 4
		t.Fatalf("bands: %+v", bands)
	}
}

// TestMailInvoiceOptions: already forwarded mails can stay out.
func TestMailInvoiceOptions(t *testing.T) {
	mail := &sources.MailDataset{Invoices: []sources.MailInvoice{{UID: 1, Amount: 10}, {UID: 2, Amount: 20}}}
	v := viewOf(t, "mail_invoices", map[string]any{"only_open": true, "limit": 1.0},
		map[string]any{"data": mail, widgets.ForwardedSlot: map[uint32]bool{1: true}}, enums.ServiceMail, nil)
	if v["Count"] != 1 || v["Sum"] != 20.0 {
		t.Fatalf("forwarded not left out: %+v", v)
	}
}
