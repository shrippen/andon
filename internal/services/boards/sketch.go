package boards

import (
	"database/sql"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
)

// Sketch is a board's floor plan for the board list: its sections, each
// with the span of every tile and the connection behind it (for its state).
//
//	Übersicht   ┌─┬─┬─┐  section, 3 columns
//	            ├─┴─┼─┤  a tile 2 wide
//	            └───┴─┘
type Sketch struct {
	Sections []SketchSection
	Tiles    int
}

// SketchSection is one section of a Sketch.
type SketchSection struct {
	Cols  int
	Tiles []SketchTile
}

// SketchTile is one tile of a SketchSection.
type SketchTile struct {
	Cols, Rows   int
	ConnectionID *int64
}

// sketchRows caps a tile's height: a tall list would fill the whole plan.
const sketchRows = 4

// sizeCols is a section's column count when it sets none: about what the
// board shows on a wide screen.
var sizeCols = map[enums.TileSize]int{enums.TileSmall: 6, enums.TileMedium: 4, enums.TileLarge: 3}

const defaultCols = 4

// Sketches returns the floor plan of each listed board, by board ID. refs
// come from Listed, so every board is one who sees.
func Sketches(d *sql.DB, refs []BoardRef) (map[int64]Sketch, error) {
	out := make(map[int64]Sketch, len(refs))
	err := db.WithRead(d, func(tx *sql.Tx) error {
		for _, ref := range refs {
			b, err := content.Board(tx, ref.ID)
			if err != nil {
				return err
			}
			if b == nil {
				continue
			}
			out[ref.ID] = sketchOf(b.Sections)
		}
		return nil
	})
	return out, err
}

func sketchOf(sections []model.Section) Sketch {
	var s Sketch
	for _, sec := range sections {
		cols := defaultCols
		if n, ok := sizeCols[sec.Size]; ok {
			cols = n
		}
		if sec.Cols != nil && *sec.Cols > 0 {
			cols = *sec.Cols
		}

		part := SketchSection{Cols: cols}
		for _, p := range sec.Placements {
			tile := SketchTile{Cols: min(max(p.Cols, 1), cols), Rows: min(max(p.Rows, 1), sketchRows)}
			if p.Widget != nil {
				tile.ConnectionID = p.Widget.ConnectionID
			}
			part.Tiles = append(part.Tiles, tile)
		}
		s.Tiles += len(part.Tiles)
		s.Sections = append(s.Sections, part)
	}
	return s
}
