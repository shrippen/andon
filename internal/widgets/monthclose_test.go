package widgets_test

import (
	"testing"

	"andon/internal/sources"
	"andon/internal/widgets"
)

// TestMonthClose: closing August on 15 Sep: unexported billable hours
// and a draft keep their steps open, an empty Paperless inbox ticks its
// step, the VAT return is listed with its due date and never ticks.
func TestMonthClose(t *testing.T) {
	kind, ok := widgets.Get("month_close")
	if !ok {
		t.Fatal("month_close not registered")
	}
	cfg, _ := widgets.Decode("month_close", map[string]any{})
	results := map[string]any{
		"kimai": &sources.KimaiDataset{Timesheets: []sources.KimaiSheet{
			{Begin: "2026-08-20", Minutes: 120, Billable: true}, {Begin: "2026-07-20", Minutes: 60, Billable: true}}},
		"invoiceninja": &sources.NinjaDataset{Invoices: []sources.NinjaInvoice{{ID: 1, Status: "draft", Date: "2026-08-31"}}},
		"paperless":    &sources.PaperlessDataset{Inbox: 0},
	}
	settings := map[string]any{"tax": map[string]any{"vat": map[string]any{"method": "ist", "return_interval": "monthly"}}}
	view := kind.View(cfg, results, ctxFor("", settings))
	if view["Month"] != "08/2026" {
		t.Fatalf("month: %v", view["Month"])
	}
	steps := map[string]widgets.CloseStep{}
	for _, s := range view["Steps"].([]widgets.CloseStep) {
		steps[s.Key] = s
	}
	if s := steps["hours"]; s.Done || s.Hours != 2 {
		t.Fatalf("hours: %+v", s)
	}
	if s := steps["drafts"]; s.Done || s.Count != 1 {
		t.Fatalf("drafts: %+v", s)
	}
	if s := steps["inbox"]; !s.Done {
		t.Fatalf("inbox: %+v", s)
	}
	if s := steps["vat"]; s.Done || s.Due != "2026-09-10" {
		t.Fatalf("vat: %+v", s)
	}
	if _, found := steps["receipts"]; found {
		t.Fatal("receipts step without Sure")
	}
}

// TestMonthCloseReceiptsLink: "name business accounts" opens the rules
// tab of the space settings, where the accounts are entered, not the
// default tab (start page).
func TestMonthCloseReceiptsLink(t *testing.T) {
	kind, _ := widgets.Get("month_close")
	cfg, _ := widgets.Decode("month_close", map[string]any{})
	results := map[string]any{"sure": &sources.SureDataset{}}
	view := kind.View(cfg, results, ctxFor("", map[string]any{}))
	for _, s := range view["Steps"].([]widgets.CloseStep) {
		if s.Key != "receipts" {
			continue
		}
		if want := "/spaces/settings?section=rules#rule-cross.expense_unrecorded"; s.URL != want {
			t.Fatalf("receipts URL %q, want %q", s.URL, want)
		}
		return
	}
	t.Fatal("no receipts step")
}
