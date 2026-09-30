package closeticks

import (
	"database/sql"

	"andon/internal/repos/users"
	"andon/internal/services/access"
	"andon/internal/widgets"
)

// Of returns the user's ticked steps per month ("2026-08").
func Of(d *sql.DB, who *access.Principal) (map[string][]string, error) {
	u, err := users.Get(d, who.UserID)
	if err != nil || u == nil {
		return nil, err
	}
	return widgets.CloseTicksOf(u.Prefs[widgets.CloseTicksPref]), nil
}
