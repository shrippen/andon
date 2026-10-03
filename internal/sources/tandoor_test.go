package sources

import "testing"

// Both API generations parse alike: 2.x pages, 1.x plain lists; checked
// entries drop out, the list is sorted by category.
func TestParseTandoor(t *testing.T) {
	items := map[string]any{"results": []any{
		map[string]any{"id": 3.0, "checked": false, "amount": 2.0, "food": map[string]any{"name": "Milch", "supermarket_category": map[string]any{"name": "Kühlregal"}},
			"unit": map[string]any{"name": "l"}, "list_recipe_data": map[string]any{"recipe_name": "Pfannkuchen"}},
		map[string]any{"id": 4.0, "checked": true, "food": map[string]any{"name": "Salz"}},
		map[string]any{"id": 5.0, "checked": false, "food": map[string]any{"name": "Äpfel", "supermarket_category": map[string]any{"name": "Obst"}}},
	}}
	meals := []any{map[string]any{"date": "2026-10-03", "recipe": map[string]any{"name": "Suppe"}, "meal_type_name": "Mittag"}}
	d := parseTandoor("https://t", items, meals)
	if len(d.Items) != 2 || d.Items[0].Food != "Milch" || d.Items[0].Recipe != "Pfannkuchen" || d.Items[1].Category != "Obst" {
		t.Fatalf("items %+v", d.Items)
	}
	if len(d.Meals) != 1 || d.Meals[0] != (Meal{Day: "2026-10-03", Title: "Suppe", Type: "Mittag"}) {
		t.Fatalf("meals %+v", d.Meals)
	}
}
