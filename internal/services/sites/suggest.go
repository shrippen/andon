package sites

// A name for a new place, from Dawarich's own reverse geocoder: Andon
// reaches no geocoder of its own, and the user chose Dawarich's.

import (
	"context"
	"database/sql"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
)

// metresPerKM converts a place's radius to the geocoder's unit.
const metresPerKM = 1000.0

// SuggestName is what Dawarich calls the position, "" when it knows
// nothing. Requires USE.
func SuggestName(ctx context.Context, d *sql.DB, who *access.Principal, connID int64, lat, lon float64) (string, error) {
	view, err := connections.Get(d, who, connID)
	if err != nil {
		return "", err
	}
	if view.Service != enums.ServiceDawarich {
		return "", ErrNotDawarich
	}
	conn, err := connections.ByID(d, connID)
	if err != nil || conn == nil {
		return "", ErrNotDawarich
	}
	params := map[string]any{"lat": lat, "lon": lon, "radius": defaultRadius / metresPerKM}
	res, err := svcdata.Get(ctx, d, sources.DawarichNearbySource.Key(), params, conn, model.UserHolder(who.UserID), svcdata.Cached)
	if err != nil {
		return "", err
	}
	near, _ := res.Data.(*sources.DawarichNearby)
	if near == nil {
		return "", nil
	}
	return near.Name, nil
}
