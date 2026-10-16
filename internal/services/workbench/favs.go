package workbench

import (
	"database/sql"
	"slices"

	"andon/internal/services/access"
)

// favsKey keeps the benches the viewer pinned to the menu bar:
//
//	User.Prefs["bench_favs"] = {"chosen": true, "ids": ["receipts", "vat"]}
//
// Until the viewer stars one, Chosen is false and the bar shows the
// benches with open hints instead.
const favsKey = "bench_favs"

// Favs are the benches pinned to the menu bar.
type Favs struct {
	Chosen bool     `json:"chosen"`
	IDs    []string `json:"ids"`
}

// LoadFavs reads the viewer's pinned benches.
func LoadFavs(d *sql.DB, who *access.Principal) (Favs, error) {
	return LoadState[Favs](d, who, favsKey)
}

// ToggleFav pins or unpins a bench. shown is what the bar shows now, the
// start when the viewer had not chosen yet.
func ToggleFav(d *sql.DB, who *access.Principal, id string, shown []string) error {
	return SaveState(d, who, favsKey, func(f *Favs) {
		if !f.Chosen {
			f.Chosen, f.IDs = true, slices.Clone(shown)
		}
		if i := slices.Index(f.IDs, id); i >= 0 {
			f.IDs = slices.Delete(f.IDs, i, i+1)
			return
		}
		f.IDs = append(f.IDs, id)
	})
}
