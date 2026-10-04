package widgets

// limitKey is the field that caps how many entries a list tile shows.
const limitKey = "limit"

// ForRows is a placed tile's config for its height: a tile spanning
// more grid rows shows more entries, so the extra height holds content
// rather than empty space. The stored config is left untouched.
//
//	rss {limit: 8}, rows 2   →   {limit: 16}   (within the field's Max)
func ForRows(typeKey string, config map[string]any, rows int) map[string]any {
	if rows <= 1 {
		return config
	}
	kind, ok := registry[typeKey]
	if !ok {
		return config
	}

	var field *Field
	for i := range kind.Fields {
		if kind.Fields[i].Key == limitKey {
			field = &kind.Fields[i]
		}
	}
	if field == nil {
		return config
	}

	// Copy, then scale the limit as the tile would read it.
	grown := make(map[string]any, len(config)+1)
	for k, v := range config {
		grown[k] = v
	}
	limit := Raw{m: config, fields: kind.Fields}.Int(limitKey) * rows
	if hi, ok := bound(field.Max); ok {
		limit = min(limit, int(hi))
	}
	grown[limitKey] = limit
	return grown
}

// Width is how wide a tile type is laid out in its section.
type Width string

const (
	WidthCell Width = ""     // one grid column, two when the tile is set wide
	WidthFull Width = "full" // the section's whole width; only its height can grow
)
