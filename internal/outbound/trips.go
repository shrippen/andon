package outbound

// Trips of the Kimai mileage plugin: a ride whose class the user set by
// hand becomes one, or changes its purpose (services/sites).

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"andon/internal/drivers/services"
)

// MileageTrip is a trip of the plugin; Vehicle "" takes the default.
type MileageTrip struct {
	Departure, Arrival time.Time
	Purpose            string // business, commute, private
	KM                 float64
	Start, Destination string
	Vehicle            string
}

func (t MileageTrip) body() map[string]any {
	out := map[string]any{
		"date":       t.Departure.Format(time.DateOnly),
		"departure":  t.Departure.Format(time.RFC3339),
		"arrival":    t.Arrival.Format(time.RFC3339),
		"purpose":    t.Purpose,
		"distanceKm": t.KM,
	}
	if t.Start != "" {
		out["start"] = t.Start
	}
	if t.Destination != "" {
		out["destination"] = t.Destination
	}
	if t.Vehicle != "" {
		out["vehicle"] = t.Vehicle
	}
	return out
}

// KimaiCreateTrip adds a trip and returns its id.
func KimaiCreateTrip(ctx context.Context, to Target, t MileageTrip) (int64, error) {
	api := services.KimaiApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
	raw, err := api.Send(ctx, http.MethodPost, "mileage/trips", t.body())
	if err != nil {
		return 0, err
	}
	return idOf(raw), nil
}

// KimaiTripPurpose changes a trip's purpose.
func KimaiTripPurpose(ctx context.Context, to Target, id int64, purpose string) error {
	api := services.KimaiApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
	_, err := api.Send(ctx, http.MethodPatch, "mileage/trips/"+strconv.FormatInt(id, 10), map[string]any{"purpose": purpose})
	return err
}
