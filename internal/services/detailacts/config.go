package detailacts

// Actions that change the tile itself; they need the right to edit it.

import (
	"context"
	"database/sql"
	"maps"
	"strings"

	"andon/internal/services/access"
	"andon/internal/services/widgetlib"
	"andon/internal/widgets"
)

func init() {
	register("custom_api", "add_field", addAPIField)
}

// addAPIField adds a value of the answer as a field ("temp = main.temp").
func addAPIField(_ context.Context, d *sql.DB, who *access.Principal, c Call) error {
	path := strings.TrimSpace(c.Form.Get("path"))
	if path == "" {
		return ErrBadMark
	}
	w := c.Widget
	cfg := maps.Clone(w.Config)
	fields, _ := cfg["fields"].(string)
	cfg["fields"] = widgets.AddAPIField(fields, path)
	return widgetlib.Update(d, who, w.ID, w.Version, w.Title, cfg, w.ConnectionID, w.MinTeamRole)
}
