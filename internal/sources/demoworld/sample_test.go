package demoworld

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestSampleMatchesWorld: sample.json (release builds) has every id the
// demo datasets look up, and none of Studio Weber's names. After
// sync-demo.py changed world.json, run demo/make-sample.py.
func TestSampleMatchesWorld(t *testing.T) {
	raw, err := os.ReadFile("sample.json")
	if err != nil {
		t.Fatal(err)
	}
	var sample World
	if err := json.Unmarshal(raw, &sample); err != nil {
		t.Fatal(err)
	}
	w := Get()

	ids := func(n int, id func(int) string) []string {
		out := make([]string, n)
		for i := range n {
			out[i] = id(i)
		}
		return out
	}
	pairs := map[string][2][]string{
		"people":     {ids(len(w.People), func(i int) string { return w.People[i].ID }), ids(len(sample.People), func(i int) string { return sample.People[i].ID })},
		"customers":  {ids(len(w.Customers), func(i int) string { return w.Customers[i].ID }), ids(len(sample.Customers), func(i int) string { return sample.Customers[i].ID })},
		"projects":   {ids(len(w.Projects), func(i int) string { return w.Projects[i].ID }), ids(len(sample.Projects), func(i int) string { return sample.Projects[i].ID })},
		"activities": {ids(len(w.Activities), func(i int) string { return w.Activities[i].ID }), ids(len(sample.Activities), func(i int) string { return sample.Activities[i].ID })},
		"places":     {ids(len(w.Places), func(i int) string { return w.Places[i].ID }), ids(len(sample.Places), func(i int) string { return sample.Places[i].ID })},
		"vendors":    {ids(len(w.Vendors), func(i int) string { return w.Vendors[i].ID }), ids(len(sample.Vendors), func(i int) string { return sample.Vendors[i].ID })},
	}
	for name, p := range pairs {
		if strings.Join(p[0], ",") != strings.Join(p[1], ",") {
			t.Errorf("%s: world %v, sample %v (run demo/make-sample.py)", name, p[0], p[1])
		}
	}
	if len(w.Receipts) != len(sample.Receipts) || len(w.Inventory.Assets) != len(sample.Inventory.Assets) {
		t.Error("receipts or assets differ (run demo/make-sample.py)")
	}

	text := string(raw)
	for _, name := range []string{w.Studio.Name, w.Studio.Domain, w.Customers[0].Name, w.People[0].Name, w.Vendors[0].Name} {
		if strings.Contains(text, name) {
			t.Errorf("sample.json contains %q", name)
		}
	}
}
