package widgets_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// keptForTiles: two things, an agenda with a check due soon, an overdue
// return and a warranty past the horizon.
func keptForTiles() *sources.KeptDataset {
	return &sources.KeptDataset{URL: "https://kept.example/",
		Things: []sources.KeptThing{{ID: "a", Code: "H6ZQ46", Name: "Feuerlöscher", Price: 49}, {ID: "b", Name: "Akkuschrauber", Price: 129.5}},
		Agenda: []sources.KeptDue{
			{Kind: "thing_expiry", State: "expiring", Due: "2026-10-01", Subject: "Feuerlöscher", Code: "H6ZQ46"},
			{Kind: "loan", State: "overdue", Due: "2026-08-20", Subject: "Leiter"},
			{Kind: "warranty", State: "upcoming", Due: "2027-06-01", Subject: "Akkuschrauber"},
		}}
}

// TestTableAssetDatesKept: Kept's agenda in the table, soonest first,
// the late one however far back, each linked to its page in Kept.
func TestTableAssetDatesKept(t *testing.T) {
	kind, _ := widgets.Get("table")
	cfg, _ := widgets.Decode("table", map[string]any{"table": "asset_dates"})
	view := kind.View(cfg, map[string]any{"data": keptForTiles()}, ctxFor(enums.ServiceKept, nil))
	rows, ok := view["Rows"].([]widgets.Row)
	if !ok || len(rows) != 2 {
		t.Fatalf("rows: %+v", view)
	}
	late, soon := rows[0].Values, rows[1].Values
	if late[0] != (widgets.Linked{Text: "Leiter", Href: "https://kept.example/expiring"}) || late[1] != "loan" {
		t.Errorf("late: %+v", late)
	}
	if soon[0] != (widgets.Linked{Text: "Feuerlöscher", Href: "https://kept.example/t/H6ZQ46"}) || soon[2] != "2026-10-01" {
		t.Errorf("soon: %+v", soon)
	}
}

// TestTableAssetDatesSnipe: Snipe-IT keeps its warranty, end-of-life and
// licence dates through the inventory shape.
func TestTableAssetDatesSnipe(t *testing.T) {
	kind, _ := widgets.Get("table")
	cfg, _ := widgets.Decode("table", map[string]any{"table": "asset_dates"})
	data := &sources.SnipeDataset{URL: "https://snipe.example",
		Assets:   []sources.SnipeAsset{{ID: 7, Name: "Laptop", WarrantyExpires: "2026-10-10", EOLDate: "2030-01-01"}},
		Licenses: []sources.SnipeLicense{{ID: 3, Name: "Office", Expires: "2026-09-30"}}}
	view := kind.View(cfg, map[string]any{"data": data}, ctxFor(enums.ServiceSnipeIT, nil))
	rows, _ := view["Rows"].([]widgets.Row)
	if len(rows) != 2 || rows[0].Values[1] != "license" || rows[1].Values[0] != (widgets.Linked{Text: "Laptop", Href: "https://snipe.example/hardware/7"}) {
		t.Fatalf("rows: %+v", rows)
	}
}

// TestKpiAssetValueKept: the value KPI counts any inventory.
func TestKpiAssetValueKept(t *testing.T) {
	kind, _ := widgets.Get("kpi")
	cfg, _ := widgets.Decode("kpi", map[string]any{"metric": "asset_value"})
	view := kind.View(cfg, map[string]any{"data": keptForTiles()}, ctxFor(enums.ServiceKept, nil))
	kpi, _ := view["KPI"].(*widgets.KpiResult)
	if kpi == nil || kpi.Value != 178.5 || kpi.SubCount != 2 {
		t.Fatalf("kpi: %+v", view)
	}
}
