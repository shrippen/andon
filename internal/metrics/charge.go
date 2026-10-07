package metrics

// Charging for business trips: the energy of an EVCC session drives the
// car until the next session, so the session's cost is shared by the km
// of the car rides in between (Dawarich), business and other.
//
//	session 10.09. 6 € ──► rides until 20.09.: 60 km business, 20 km private
//	                       ⇒ 6 € × 60/80 = 4,50 € for business trips

import (
	"sort"
	"time"

	"andon/internal/sources"
)

// ChargeShare is the sessions of a period and their business share.
type ChargeShare struct {
	Sessions       int
	KWh, Cost      float64
	KM, BusinessKM float64 // car km after the sessions
	BusinessCost   float64
}

// ChargeBusiness shares the cost of the sessions that finished in
// [from, to] (days, inclusive) by the car rides after each of them.
func ChargeBusiness(sessions []sources.EVCCSession, rides []ClassedRide, from, to time.Time) ChargeShare {
	list := append([]sources.EVCCSession(nil), sessions...)
	sort.Slice(list, func(i, j int) bool { return list[i].Finished.Before(list[j].Finished) })
	end := to.AddDate(0, 0, 1)

	var out ChargeShare
	for i, s := range list {
		if s.Finished.Before(from) || !s.Finished.Before(end) {
			continue
		}
		until := time.Time{} // the next session, or open
		if i+1 < len(list) {
			until = list[i+1].Created
		}
		km, business := carKM(rides, s.Finished, until)
		out.Sessions++
		out.KWh += s.KWh
		out.Cost += s.Price
		out.KM += km
		out.BusinessKM += business
		if km > 0 {
			out.BusinessCost += s.Price * business / km
		}
	}
	return out
}

// carKM sums the km of payable rides (own car) starting in [from, until),
// all and business; a zero until is open.
func carKM(rides []ClassedRide, from, until time.Time) (km, business float64) {
	for _, r := range rides {
		if !Payable(r.Ride) || r.Start.Before(from) || (!until.IsZero() && !r.Start.Before(until)) {
			continue
		}
		km += r.KM
		if r.Class == ClassBusiness {
			business += r.KM
		}
	}
	return km, business
}
