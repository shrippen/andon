package metrics_test

import (
	"testing"

	"andon/internal/metrics"
	"andon/internal/sources"
)

// Places of the Kimai mileage plugin map areas by id; the connection's
// "areas" option overrides them by name.
func TestAreaMapMergesPlacesAndOption(t *testing.T) {
	geo := &sources.DawarichDataset{Areas: []sources.DawarichArea{{ID: 7, Name: "Muster"}, {ID: 8, Name: "Home"}, {ID: 9, Name: "Weber"}}}
	kimai := &sources.KimaiDataset{Places: []sources.KimaiPlace{
		{AreaID: 7, Type: "customer", CustomerID: 12},
		{AreaID: 8, Type: "home"},
		{AreaID: 9, Type: "customer", CustomerID: 3},
		{AreaID: 99, Type: "customer", CustomerID: 4},
		{Type: "customer", CustomerID: 5},
	}}
	options := map[string]any{"areas": map[string]any{"Weber": map[string]any{"customer_id": 30.0}}}

	got := metrics.AreaMap(geo, kimai, options)

	want := map[string]metrics.AreaMapping{"Muster": {CustomerID: 12}, "Home": {Home: true}, "Weber": {CustomerID: 30}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for name, m := range want {
		if got[name] != m {
			t.Fatalf("%s: got %v, want %v", name, got[name], m)
		}
	}
}

// Without Kimai or Dawarich data only the option counts.
func TestAreaMapWithoutPlaces(t *testing.T) {
	options := map[string]any{"areas": map[string]any{"Home": map[string]any{"home": true}}}

	got := metrics.AreaMap(nil, nil, options)

	if !got["Home"].Home || len(got) != 1 {
		t.Fatalf("got %v", got)
	}
}
