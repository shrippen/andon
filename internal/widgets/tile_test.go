package widgets

import "testing"

// decodeOf decodes raw through the registered type key, as C.
func decodeOf[C any](key string, raw map[string]any) C {
	cfg, _ := Decode(key, raw)
	return configOf[C](cfg)
}

// run decodes raw through the registered type key and renders its view.
func run(key string, raw, results map[string]any, ctx ViewCtx) map[string]any {
	cfg, _ := Decode(key, raw)
	return registry[key].View(cfg, results, ctx)
}

// Raw applies a field's default and bounds, and keeps selects to their
// options.
func TestRawFields(t *testing.T) {
	fields := []Field{{Key: "limit", Input: InputNumber, Default: 8, Min: "1", Max: "50"},
		{Key: "rate", Input: InputNumber, Default: 0.3, Min: "0"},
		{Key: "shown", Input: InputCheck, Default: true}, sel("sort", "next", "next", "price")}
	read := func(m map[string]any) Raw { return Raw{m: m, fields: fields} }

	empty := read(map[string]any{})
	if empty.Int("limit") != 8 || empty.Float("rate") != 0.3 || !empty.Bool("shown") || empty.Pick("sort") != "next" {
		t.Fatalf("defaults: %d %v %v %q", empty.Int("limit"), empty.Float("rate"), empty.Bool("shown"), empty.Pick("sort"))
	}
	out := read(map[string]any{"limit": 80.0, "rate": -1.0, "shown": false, "sort": "size"})
	if out.Int("limit") != 50 || out.Float("rate") != 0 || out.Bool("shown") || out.Pick("sort") != "next" {
		t.Fatalf("bounds: %d %v %v %q", out.Int("limit"), out.Float("rate"), out.Bool("shown"), out.Pick("sort"))
	}
	if read(map[string]any{"limit": 0.0}).Int("limit") != 1 || read(map[string]any{"sort": "price"}).Pick("sort") != "price" {
		t.Fatal("low bound or valid pick")
	}
}

// Every type is declared as a Tile, so its fields, topic and calm check
// sit with its code.
func TestEveryTypeIsTile(t *testing.T) {
	for _, kind := range AllTypes() {
		if !kind.tile {
			t.Errorf("%s is not a Tile", kind.Key)
		}
	}
}
