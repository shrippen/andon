package widgetlib

import (
	"database/sql"

	"andon/internal/db"
	"andon/internal/repos/content"
	"andon/internal/widgets"
)

// UpgradeConfigs rewrites stored widget configs under current key names
// (widgets.Upgrade), e.g. komodo's only_issues → only_problems. Runs at
// start; a config already current is left alone, version included.
// Returns how many widgets changed.
func UpgradeConfigs(d *sql.DB) (int, error) {
	changed := 0
	err := db.WithTx(d, func(tx *sql.Tx) error {
		list, err := content.WidgetsOfTypes(tx, widgets.RenamedTypes())
		if err != nil {
			return err
		}
		for _, w := range list {
			config, moved := widgets.Upgrade(w.Type, w.Config)
			if !moved {
				continue
			}
			if err := content.SetWidgetConfig(tx, w.ID, config); err != nil {
				return err
			}
			changed++
		}
		return nil
	})
	return changed, err
}
