package sites

// The learned ride classes (metrics/ridelearn.go), one model per Dawarich
// connection and holder (services/learn):
//
//	SetClass ──────────────► learn.Learn (the ride, its class; back to the rules forgets)
//	job ── option "rides" ─► teachRides (classes set by hand so far)
//	travel tile ◄─────────── Learned

import (
	"database/sql"

	"andon/internal/bayes"
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/services/learn"
	"andon/internal/sources"
)

// PurposeRides is the model proposing a ride's class.
const PurposeRides = "ride_class"

func init() {
	learn.Register(learn.Purpose{Key: PurposeRides, Service: enums.ServiceDawarich, Teach: teachRides})
}

// teachRides learns the classes set by hand in a Dawarich connection's
// option; those the plugin holds were learned when they were set.
func teachRides(m *bayes.Model, data any, conn *model.Connection) bool {
	geo, _ := data.(*sources.DawarichDataset)
	return metrics.TeachRides(m, geo, conn.Options)
}

// Learned is the ride model of a Dawarich connection, as h reads it.
func Learned(d *sql.DB, conn *model.Connection, h model.Holder) bayes.Use {
	return learn.Model(d, PurposeRides, conn, h)
}
