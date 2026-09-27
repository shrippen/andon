package widgets_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// TestTravelTile: this month's and year's distance from Dawarich's own
// statistics (months keyed by English name), places of the year.
func TestTravelTile(t *testing.T) {
	kind, ok := widgets.Get("travel")
	if !ok {
		t.Fatal("travel not registered")
	}
	cfg, _ := widgets.Decode("travel", map[string]any{})
	data := &sources.DawarichDataset{Stats: map[string]any{"yearlyStats": []any{
		map[string]any{"year": 2026.0, "totalDistanceKm": 3100.0, "totalCountriesVisited": 2.0, "totalCitiesVisited": 14.0,
			"monthlyDistanceKm": map[string]any{"august": 500.0, "september": 412.0}},
		map[string]any{"year": 2025.0, "totalDistanceKm": 9000.0},
	}}}
	view := kind.View(cfg, map[string]any{"data": data}, ctxFor(enums.ServiceDawarich, nil))
	if view["MonthKM"] != 412.0 || view["YearKM"] != 3100.0 || view["Cities"] != 14 || view["Countries"] != 2 {
		t.Fatalf("view: %+v", view)
	}
	if view["PrevKM"] != 500.0 {
		t.Fatalf("previous month: %v", view["PrevKM"])
	}
}

// TestTravelYear: the year against last year, and no bar when asked.
func TestTravelYear(t *testing.T) {
	data := &sources.DawarichDataset{Stats: map[string]any{"yearlyStats": []any{
		map[string]any{"year": 2026.0, "totalDistanceKm": 3100.0, "monthlyDistanceKm": map[string]any{"august": 500.0, "september": 412.0}},
		map[string]any{"year": 2025.0, "totalDistanceKm": 9000.0},
	}}}
	v := viewOf(t, "travel", map[string]any{"period": "year"}, map[string]any{"data": data}, enums.ServiceDawarich, nil)
	if v["HeadKM"] != 3100.0 || v["PrevKM"] != 9000.0 || v["Year"] != true || v["Bar"] != 34 {
		t.Fatalf("year: %+v", v)
	}
	v = viewOf(t, "travel", map[string]any{"hide_bar": true}, map[string]any{"data": data}, enums.ServiceDawarich, nil)
	if v["HeadKM"] != 412.0 || v["Bar"] != nil {
		t.Fatalf("month without bar: %+v", v)
	}
}
