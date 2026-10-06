package detailacts

// The travel dialog's action: a ride's class set by hand (services/sites).

import (
	"context"
	"database/sql"
	"strconv"

	"andon/internal/metrics"
	"andon/internal/services/access"
	auditsvc "andon/internal/services/audit"
	"andon/internal/services/sites"
)

func init() {
	register("travel", "ride_class", rideClass)
}

// rideClass sets the class of the ride starting at "ride" (Unix seconds)
// to "class"; "" hands it back to the rules.
func rideClass(ctx context.Context, d *sql.DB, who *access.Principal, c Call) error {
	start, err := strconv.ParseInt(c.Form.Get("ride"), 10, 64)
	if err != nil || c.Widget.ConnectionID == nil {
		return ErrBadMark
	}
	class := metrics.RideClass(c.Form.Get("class"))
	if err := sites.SetClass(ctx, d, who, *c.Widget.ConnectionID, start, class); err != nil {
		return err
	}
	return auditsvc.Log(d, &who.UserID, "travel.ride_class", c.Form.Get("ride")+" "+string(class), c.IP, nil)
}
