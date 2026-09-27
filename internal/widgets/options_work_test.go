package widgets_test

import (
	"andon/internal/metrics"
	"testing"

	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// TestKimaiSplitOptions: last week grouped by project (15 Sep 2026 is a
// Tuesday, so last week starts on 7 Sep).
func TestKimaiSplitOptions(t *testing.T) {
	data := &sources.KimaiDataset{
		Projects: []sources.KimaiProject{{ID: 1, Name: "Website", CustomerID: 9}, {ID: 2, Name: "Shop", CustomerID: 9}},
		Timesheets: []sources.KimaiSheet{
			{Begin: "2026-09-07", Minutes: 60, ProjectID: 1, CustomerID: 9},
			{Begin: "2026-09-08", Minutes: 120, ProjectID: 2, CustomerID: 9},
			{Begin: "2026-09-14", Minutes: 600, ProjectID: 2, CustomerID: 9},
		}}
	v := viewOf(t, "kimai_split", map[string]any{"week": "last", "group": "project"}, map[string]any{"data": data}, enums.ServiceKimai, nil)
	legend := v["Legend"].([]widgets.SplitLegend)
	if len(legend) != 2 || legend[0].Name != "Shop" || legend[0].Hours != "2:00" || legend[1].Name != "Website" {
		t.Fatalf("legend: %+v", legend)
	}
}

// TestKimaiWeekOptions: Saturday counts as a workday and internal time is
// left out.
func TestKimaiWeekOptions(t *testing.T) {
	data := &sources.KimaiDataset{Timesheets: []sources.KimaiSheet{
		{Begin: "2026-09-14", Minutes: 120, Billable: true},
		{Begin: "2026-09-14", Minutes: 300, Billable: false},
	}}
	v := viewOf(t, "kimai_week", map[string]any{"workdays": "mo_sa", "billable_only": true, "week_hours": 48.0}, map[string]any{"data": data}, enums.ServiceKimai, nil)
	if v["Total"] != "2:00" {
		t.Fatalf("total: %v", v["Total"])
	}
	days := v["Days"].([]widgets.DayCol)
	if days[0].Tier != "yellow" {
		t.Fatalf("2 h of 8 h should be short: %+v", days[0])
	}
	v = viewOf(t, "kimai_week", map[string]any{"workdays": "mo_sa"}, map[string]any{"data": data}, enums.ServiceKimai, nil)
	if v["Total"] != "7:00" {
		t.Fatalf("all hours: %v", v["Total"])
	}
}

// TestKimaiTimerOptions: more quick starts and the description field.
func TestKimaiTimerOptions(t *testing.T) {
	live := &sources.KimaiLive{}
	for i := int64(1); i <= 8; i++ {
		live.Recent = append(live.Recent, sources.KimaiTimer{ProjectID: i, ActivityID: 1})
	}
	v := viewOf(t, "kimai_timer", map[string]any{"recent": 6.0, "ask_note": true}, map[string]any{"live": live}, enums.ServiceKimai, nil)
	if len(v["Recent"].([]widgets.TimerRow)) != 6 || v["AskNote"] != true {
		t.Fatalf("timer: %+v", v)
	}
}

// TestListOptions: Kintsugi by kind and count, Gitea only reviews.
func TestListOptions(t *testing.T) {
	k := &sources.KintsugiDataset{Open: []sources.KintsugiSuggestion{
		{ID: 1, Kind: sources.KintsugiAcquisition}, {ID: 2, Kind: sources.KintsugiDevelopment}, {ID: 3, Kind: sources.KintsugiDevelopment}}}
	v := viewOf(t, "kintsugi", map[string]any{"kind": "development", "limit": 1.0}, map[string]any{"data": k}, enums.ServiceKintsugi, nil)
	open := v["Open"].([]sources.KintsugiSuggestion)
	if len(open) != 1 || open[0].ID != 2 {
		t.Fatalf("kintsugi: %+v", open)
	}
	g := &sources.GiteaDataset{Reviews: []sources.Issue{{Title: "r"}}, Assigned: []sources.Issue{{Title: "a"}}}
	v = viewOf(t, "gitea_reviews", map[string]any{"show": "reviews"}, map[string]any{"data": g}, enums.ServiceGitea, nil)
	if v["Assigned"] != nil || len(v["Reviews"].([]sources.Issue)) != 1 {
		t.Fatalf("gitea: %+v", v)
	}
}

// TestWeekStoryOptions: money lines can go.
func TestWeekStoryOptions(t *testing.T) {
	lines := []metrics.StoryLine{{Key: "hours"}, {Key: "invoiced"}, {Key: "paid"}, {Key: "power"}}
	v := viewOf(t, "week_story", map[string]any{"show_money": false, "period": "calendar"}, map[string]any{widgets.StorySlot: lines}, "", nil)
	shown := v["Lines"].([]metrics.StoryLine)
	if len(shown) != 2 || shown[1].Key != "power" || v["Calendar"] != true {
		t.Fatalf("story: %+v", v)
	}
}

// TestPaymentDaysOptions: old invoices and hidden clients drop out.
func TestPaymentDaysOptions(t *testing.T) {
	ninja := &sources.NinjaDataset{
		Clients: []sources.NinjaClient{{ID: 1, Name: "Muster"}, {ID: 2, Name: "Verein"}},
		Invoices: []sources.NinjaInvoice{
			{ID: 1, ClientID: 1, Status: "paid", Date: "2025-01-10"},
			{ID: 2, ClientID: 1, Status: "paid", Date: "2026-08-01"},
			{ID: 3, ClientID: 2, Status: "paid", Date: "2026-08-01"},
		},
		Payments: []sources.NinjaPayment{{ClientID: 1, Date: "2025-03-10"}, {ClientID: 1, Date: "2026-08-11"}, {ClientID: 2, Date: "2026-08-05"}},
	}
	v := viewOf(t, "payment_days", map[string]any{"months": 6.0, "hide_clients": []any{"verein"}}, map[string]any{"data": ninja}, enums.ServiceInvoiceNinja, nil)
	rows := v["Rows"].([]widgets.PayRow)
	if len(rows) != 1 || rows[0].Client != "Muster" || rows[0].Count != 1 {
		t.Fatalf("rows: %+v", rows)
	}
}

// TestRateTrendBillable: the rate over billable hours only (today is
// 15 Sep 2026, so August is the last full month).
func TestRateTrendBillable(t *testing.T) {
	ninja := &sources.NinjaDataset{Invoices: []sources.NinjaInvoice{{ID: 1, ClientID: 1, Status: "sent", Date: "2026-08-10", Net: 1000, Amount: 1000}}}
	kimai := &sources.KimaiDataset{Timesheets: []sources.KimaiSheet{
		{Begin: "2026-08-03", Minutes: 600, Billable: true}, {Begin: "2026-08-04", Minutes: 600}}}
	res := map[string]any{"data": ninja, "kimai": kimai}
	all := viewOf(t, "rate_trend", map[string]any{"months": 6.0}, res, enums.ServiceInvoiceNinja, nil)
	billable := viewOf(t, "rate_trend", map[string]any{"months": 6.0, "billable_only": true}, res, enums.ServiceInvoiceNinja, nil)
	if all["Rate"] != 50.0 || billable["Rate"] != 100.0 {
		t.Fatalf("all %v, billable %v", all["Rate"], billable["Rate"])
	}
}

// TestMonthCloseOptions: the running month, drafts hidden, and a step
// ticked by hand counts as done (today is 15 Sep 2026).
func TestMonthCloseOptions(t *testing.T) {
	kimai := &sources.KimaiDataset{Timesheets: []sources.KimaiSheet{{Begin: "2026-09-02", Minutes: 90, Billable: true}}}
	ninja := &sources.NinjaDataset{Invoices: []sources.NinjaInvoice{{ID: 1, Status: "draft"}}}
	res := map[string]any{"kimai": kimai, "invoiceninja": ninja, widgets.CloseTicksPref: map[string]any{"2026-09": []any{"hours"}}}
	v := viewOf(t, "month_close", map[string]any{"month": "current", "manual": true, "close_drafts": false}, res, "", nil)
	steps := v["Steps"].([]widgets.CloseStep)
	if v["MonthKey"] != "2026-09" || len(steps) != 1 || steps[0].Key != "hours" || !steps[0].Done || !steps[0].Hand {
		t.Fatalf("close: %+v", v)
	}
}

// TestSubsOptions: one category, dearest first, per year.
func TestSubsOptions(t *testing.T) {
	wallos := &sources.WallosDataset{Subs: []sources.WallosSub{
		{Name: "VPS", Category: "Hosting", Monthly: 5, Price: 5},
		{Name: "Mail", Category: "Hosting", Monthly: 3, Price: 36},
		{Name: "Film", Category: "Privat", Monthly: 12, Price: 12},
	}}
	v := viewOf(t, "subscriptions", map[string]any{"sort": "price", "yearly": true, "categories": []any{"hosting"}}, map[string]any{"wallos": wallos}, "", nil)
	rows := v["Rows"].([]widgets.SubRow)
	if len(rows) != 2 || rows[0].Name != "VPS" || rows[0].Price != 60 || v["Monthly"] != 96.0 {
		t.Fatalf("subs: %+v", v)
	}
}
