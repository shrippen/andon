package sites

// A ride's class set by hand, for when the rules got it wrong:
//
//	car or motorbike, plugin answers ──► mileage trip in the plugin
//	                                     (new, or its purpose changed)
//	otherwise ─────────────────────────► option "rides" of the connection
//
// The ride is known by its start (Unix seconds). Back to the rules only
// clears the option: a plugin trip stays (nothing is deleted across the
// services).

import (
	"andon/internal/caps"
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/outbound"
	"andon/internal/services/access"
	"andon/internal/services/connections"
)

var (
	// ErrUnknownRide: no ride starts then (any more).
	ErrUnknownRide = errors.New("sites.unknown_ride")
	// ErrBadClass: no class of a ride.
	ErrBadClass = errors.New("sites.bad_class")
)

// tripVehicles are the plugin's vehicles per ride mode; a car takes the
// user's default vehicle.
var tripVehicles = map[string]string{metrics.ModeMotorcycle: "motorcycle"}

// SetClass sets the class of the ride starting at start; ClassAuto hands
// it back to the rules. Requires USE on Dawarich, EDIT on Kimai for the
// plugin, MANAGE on Dawarich for the option.
func SetClass(ctx context.Context, d *sql.DB, who *access.Principal, connID, start int64, class metrics.RideClass) error {
	if class != metrics.ClassAuto && !slices.Contains(metrics.RideClasses, class) {
		return ErrBadClass
	}
	e, err := open(ctx, d, who, connID, enums.RightUse)
	if err != nil {
		return err
	}
	ride, ok := e.ride(start)
	if !ok {
		return ErrUnknownRide
	}
	defer e.forget()

	if class != metrics.ClassAuto && e.rideHolder(ride.Mode) == kimaiHolder {
		if err := e.tripOf(ctx, d, who, ride, class); err != nil {
			return err
		}
		class = metrics.ClassAuto // the plugin holds it now
	}
	return e.keepClass(d, who, start, class)
}

// rideHolder is who keeps a ride's class: the plugin for the modes it
// pays (car, motorbike), Andon otherwise.
func (e env) rideHolder(mode string) caps.Holder {
	if !e.pluginLive() {
		return caps.Andon
	}
	return caps.Store(caps.Rides, caps.Update, mode, e.kimai.Caps)
}

// ride is the ride starting at start.
func (e env) ride(start int64) (metrics.ClassedRide, bool) {
	travel := metrics.TravelOf(e.geo, e.kimai, e.geoConn.Options, metrics.TravelSettingsOf(nil), time.Now())
	for _, r := range travel.Rides {
		if r.Start.Unix() == start {
			return r, true
		}
	}
	return metrics.ClassedRide{}, false
}

// tripOf writes the ride's class to the plugin: the covering trip's
// purpose, else a new trip.
func (e env) tripOf(ctx context.Context, d *sql.DB, who *access.Principal, r metrics.ClassedRide, class metrics.RideClass) error {
	to, err := e.kimaiTarget(d, who)
	if err != nil {
		return err
	}
	if r.Trip != 0 {
		return outbound.KimaiTripPurpose(ctx, to, r.Trip, string(class))
	}
	trip := outbound.MileageTrip{Departure: r.Start.In(time.Local), Arrival: r.End.In(time.Local), Purpose: string(class),
		KM: r.KM, Vehicle: tripVehicles[r.Mode]}
	if r.From != nil {
		trip.Start = r.From.Name
	}
	if r.To != nil {
		trip.Destination = r.To.Name
	}
	_, err = outbound.KimaiCreateTrip(ctx, to, trip)
	return err
}

// keepClass writes the class to the connection's option; ClassAuto
// removes it there. Nothing to remove writes nothing.
func (e env) keepClass(d *sql.DB, who *access.Principal, start int64, class metrics.RideClass) error {
	key := strconv.FormatInt(start, 10)
	old, _ := e.geoConn.Options[metrics.OptionRides].(map[string]any)
	if _, ok := old[key]; !ok && class == metrics.ClassAuto {
		return nil
	}

	options := map[string]any{}
	for k, v := range e.geoConn.Options {
		options[k] = v
	}
	all := map[string]any{}
	for k, v := range old {
		all[k] = v
	}
	if class == metrics.ClassAuto {
		delete(all, key)
	} else {
		all[key] = string(class)
	}
	options[metrics.OptionRides] = all
	return connections.SetOptions(d, who, e.geoConn.ID, options)
}
