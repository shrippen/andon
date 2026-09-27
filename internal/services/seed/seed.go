// Package seed fills a fresh instance: the demo instance (ANDON_DEMO,
// demo.go, left out of release builds) and an optional seed.yml
// (SEED_FILE) imported into the instance space.
package seed

import (
	"database/sql"
	"log/slog"
	"os"

	"andon/internal/enums"
	"andon/internal/repos/content"
	"andon/internal/repos/users"
	"andon/internal/services/access"
	"andon/internal/services/porting"
)

// FromFile imports seed.yml into the instance space once (while it has no
// boards), as the first admin.
func FromFile(d *sql.DB, path string) error {
	text, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	space, err := content.InstanceSpace(d)
	if err != nil || space == nil {
		return err
	}
	existing, err := content.Boards(d, []int64{space.ID})
	if err != nil || len(existing) > 0 {
		return err
	}

	all, err := users.All(d)
	if err != nil {
		return err
	}
	for _, u := range all {
		if u.Role != enums.RoleAdmin {
			continue
		}
		who, err := access.Load(d, u.ID)
		if err != nil {
			return err
		}
		report, err := porting.ImportSpace(d, who, space.ID, string(text), porting.Merge)
		if err != nil {
			return err
		}
		slog.Info("seed imported", "boards", report.Boards, "widgets", report.Widgets)
		return nil
	}
	slog.Warn("seed.yml waits for the first admin")
	return nil
}
