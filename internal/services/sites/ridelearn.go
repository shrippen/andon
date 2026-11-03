package sites

// The learned ride classes (metrics/ridelearn.go), one model per Dawarich
// connection and holder (services/learn):
//
//	SetClass ────────────────────────► learn.Learn (the ride, its class; back to the rules forgets)
//	job ── option "rides", Kimai trips ► teachRides (classes set by hand so far)
//	travel tile ◄───────────────────── Learned (the user's own model, as SetClass teaches it)

import (
	"database/sql"

	"andon/internal/bayes"
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/services/access"
	"andon/internal/services/learn"
	"andon/internal/sources"
)

// PurposeRides is the model proposing a ride's class.
const PurposeRides = "ride_class"

func init() {
	learn.Register(learn.Purpose{Key: PurposeRides, Service: enums.ServiceDawarich, Teach: teachRides, Peer: enums.ServiceKimai})
}

// teachRides learns the classes set by hand of a Dawarich connection's
// rides: its option and the trips in its Kimai's mileage plugin.
func teachRides(m *bayes.Model, data any, conn *model.Connection) bool {
	p, _ := data.(learn.Peered)
	geo, _ := p.Data.(*sources.DawarichDataset)
	kimai, _ := p.Peer.(*sources.KimaiDataset)
	return metrics.TeachRides(m, geo, kimai, conn.Options)
}

// Learned is who's ride model of a Dawarich connection: the one SetClass
// teaches, wherever the tile is shown.
func Learned(d *sql.DB, conn *model.Connection, who *access.Principal) bayes.Use {
	return learn.Model(d, PurposeRides, conn, model.UserHolder(who.UserID))
}
