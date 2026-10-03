package sources

// Tandoor Recipes: the open shopping list and the week's meal plan.
//
//	GET /api/shopping-list-entry/?checked=false   open entries
//	GET /api/meal-plan/?from_date=…&to_date=…     planned meals
//
// Tandoor 1.x answers plain lists, 2.x pages ({"results": […]}).

import (
	"context"
	"net/url"
	"sort"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

const (
	tandoorPageSize = "200"
	// TandoorPlanDays is how far the meal plan reaches (today included).
	TandoorPlanDays = 7
)

// ShopItem is one open entry of the shopping list.
type ShopItem struct {
	ID       int64
	Food     string
	Amount   float64
	Unit     string
	Category string // supermarket category, "" = none
	Recipe   string // the recipe it was added for, "" = by hand
}

// Meal is one planned meal.
type Meal struct {
	Day   string // "2026-10-03"
	Title string // the recipe, or the plan's own title
	Type  string // "Mittagessen"
}

type TandoorDataset struct {
	URL   string
	Items []ShopItem
	Meals []Meal
}

var TandoorData = source{key: "tandoor.data", ttl: opsTTL, service: enums.ServiceTandoor, fetch: fetchTandoor}

func fetchTandoor(ctx context.Context, sctx Ctx) (any, error) {
	now := time.Now()
	if isDemo(sctx) {
		return DemoTandoor(now), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.BearerApi(sctx.URL, secret, sctx.TLS())
	items, err := api.Get(ctx, "api/shopping-list-entry/", url.Values{"checked": {"false"}, "page_size": {tandoorPageSize}})
	if err != nil {
		return nil, fetchError(err)
	}
	from := now.Format(time.DateOnly)
	to := now.AddDate(0, 0, TandoorPlanDays-1).Format(time.DateOnly)
	meals, err := api.Get(ctx, "api/meal-plan/", url.Values{"from_date": {from}, "to_date": {to}, "page_size": {tandoorPageSize}})
	if err != nil {
		return nil, fetchError(err)
	}
	return parseTandoor(sctx.URL, items, meals), nil
}

// tandoorList unwraps 2.x pages; 1.x lists pass as they are.
func tandoorList(body any) []any {
	if page, ok := body.(map[string]any); ok {
		return asList(page["results"])
	}
	return asList(body)
}

// parseTandoor reads both answers; checked entries (older servers ignore
// the filter) are left out.
func parseTandoor(base string, items, meals any) *TandoorDataset {
	data := &TandoorDataset{URL: base}
	for _, raw := range tandoorList(items) {
		m := asMap(raw)
		if asBool(m["checked"]) {
			continue
		}
		food := asMap(m["food"])
		recipe := firstStr(asStr(asMap(m["list_recipe_data"])["recipe_name"]), asStr(asMap(m["recipe_mealplan"])["recipe_name"]))
		data.Items = append(data.Items, ShopItem{ID: asInt64(m["id"]), Food: asStr(food["name"]), Amount: asFloat(m["amount"]),
			Unit: asStr(asMap(m["unit"])["name"]), Category: asStr(asMap(food["supermarket_category"])["name"]), Recipe: recipe})
	}
	sort.SliceStable(data.Items, func(i, j int) bool {
		if data.Items[i].Category != data.Items[j].Category {
			return data.Items[i].Category < data.Items[j].Category
		}
		return data.Items[i].Food < data.Items[j].Food
	})
	for _, raw := range tandoorList(meals) {
		m := asMap(raw)
		day := dateOf(firstStr(asStr(m["from_date"]), asStr(m["date"])))
		title := firstStr(asStr(asMap(m["recipe"])["name"]), asStr(m["title"]))
		kind := firstStr(asStr(asMap(m["meal_type"])["name"]), asStr(m["meal_type_name"]))
		data.Meals = append(data.Meals, Meal{Day: day, Title: title, Type: kind})
	}
	sort.SliceStable(data.Meals, func(i, j int) bool { return data.Meals[i].Day < data.Meals[j].Day })
	return data
}

// dateOf is the date part of "2026-10-03T00:00:00+02:00" or "2026-10-03".
func dateOf(s string) string { return s[:min(len(s), len(time.DateOnly))] }

// DemoTandoor is the demo shopping list: what the demo Grocy misses, and
// the week's plan.
func DemoTandoor(now time.Time) *TandoorDataset {
	data := &TandoorDataset{}
	demoworld.MustDecode("kitchen", now, data)
	return data
}

func init() {
	Register(TandoorData)
	Register(testOf{TandoorData, func(d any) map[string]any { return map[string]any{"items": len(d.(*TandoorDataset).Items)} }})
}
