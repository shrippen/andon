// Classify: each ride is business, commute or private, with the reason.
// The first rule that matches wins:
//
//	1 plugin    a Kimai mileage trip covers half the ride   → its purpose
//	2 kimai     half the ride lies in booked Kimai time     → business
//	3 customer  it starts or ends at a customer site        → business
//	4 commute   home ↔ work (only with a place of work)     → commute
//	5 rest                                                  → private
//
// Afterwards a private ride between two business rides of the same day
// becomes business (reason chain): customer A → bakery → customer B.
package metrics

import (
	"time"

	"andon/internal/sources"
)

// RideClass is what a ride was for.
type RideClass string

const (
	ClassPrivate  RideClass = "private"
	ClassBusiness RideClass = "business"
	ClassCommute  RideClass = "commute"
)

// RideClasses in display order.
var RideClasses = []RideClass{ClassBusiness, ClassCommute, ClassPrivate}

// RideReason is the rule that classified a ride.
type RideReason string

const (
	ReasonRest     RideReason = "rest"
	ReasonPlugin   RideReason = "plugin"
	ReasonKimai    RideReason = "kimai"
	ReasonCustomer RideReason = "customer"
	ReasonCommute  RideReason = "commute"
	ReasonChain    RideReason = "chain"
)

// TravelBase is where business travel starts for tax purposes.
type TravelBase string

const (
	// BaseHome: home is the place of business (freelancer); no commute.
	BaseHome TravelBase = "home"
	// BaseWork: a separate place of work; home ↔ work is commuting.
	BaseWork TravelBase = "work"
)

// coverShare is the share of a ride that booked time or a plugin trip
// must cover.
const coverShare = 0.5

// Plugin trip purposes.
const (
	purposeBusiness = "business"
	purposeCommute  = "commute"
	purposePrivate  = "private"
)

var purposeClass = map[string]RideClass{purposeBusiness: ClassBusiness, purposeCommute: ClassCommute, purposePrivate: ClassPrivate}

// ClassedRide is a ride with its sites and class. CustomerID is 0 when
// unknown; Unconfirmed marks a customer-site ride without Kimai time for
// that customer on its day.
type ClassedRide struct {
	Ride
	From, To    *Site
	Class       RideClass
	Reason      RideReason
	CustomerID  int64
	Unconfirmed bool
}

// Day is the ride's local day.
func (r ClassedRide) Day() time.Time {
	s := r.Start.In(time.Local)
	return time.Date(s.Year(), s.Month(), s.Day(), 0, 0, 0, 0, time.UTC)
}

// span is a time range [from, to) with what it belongs to.
type span struct {
	from, to time.Time
	customer int64
	class    RideClass
}

// overlap is how long [a, b) and s share.
func (s span) overlap(a, b time.Time) time.Duration {
	from, to := s.from, s.to
	if a.After(from) {
		from = a
	}
	if b.Before(to) {
		to = b
	}
	return max(to.Sub(from), 0)
}

// bestCover is the span covering most of [a, b) if it covers at least
// coverShare of it; spans are assumed not to overlap each other much.
func bestCover(spans []span, a, b time.Time) (span, bool) {
	total := b.Sub(a)
	if total <= 0 {
		return span{}, false
	}
	var best span
	var bestD, sum time.Duration
	for _, s := range spans {
		d := s.overlap(a, b)
		sum += d
		if d > bestD {
			best, bestD = s, d
		}
	}
	return best, bestD > 0 && float64(sum) >= coverShare*float64(total)
}

func sheetSpans(kimai *sources.KimaiDataset, now time.Time) []span {
	var out []span
	for _, list := range [][]sources.KimaiSheet{kimai.Timesheets, kimai.Active} {
		for _, s := range list {
			from, ok := ParseTime(s.Begin)
			if !ok {
				continue
			}
			to, ok := ParseTime(s.End)
			if !ok {
				to = now // running
			}
			out = append(out, span{from: from, to: to, customer: s.CustomerID})
		}
	}
	return out
}

func pluginSpans(kimai *sources.KimaiDataset) []span {
	customers := map[int64]int64{}
	for _, p := range kimai.Projects {
		customers[p.ID] = p.CustomerID
	}
	var out []span
	for _, t := range kimai.MileageTrips {
		from, ok1 := ParseTime(t.Departure)
		to, ok2 := ParseTime(t.Arrival)
		class, known := purposeClass[t.Purpose]
		if !ok1 || !ok2 || !known {
			continue
		}
		out = append(out, span{from: from, to: to, customer: customers[t.Project], class: class})
	}
	return out
}

// bookedDays is which customer has Kimai time on which day.
func bookedDays(kimai *sources.KimaiDataset) map[[2]any]bool {
	out := map[[2]any]bool{}
	for _, s := range kimai.Timesheets {
		if d, ok := ParseDay(s.Begin); ok {
			out[[2]any{d, s.CustomerID}] = true
		}
	}
	return out
}

// Classify sorts rides into business, commute and private; kimai may be
// nil (rules 1 and 2 then do not apply).
func Classify(rides []Ride, book *Book, kimai *sources.KimaiDataset, base TravelBase, now time.Time) []ClassedRide {
	var sheets, plugin []span
	var booked map[[2]any]bool
	if kimai != nil {
		sheets, plugin, booked = sheetSpans(kimai, now), pluginSpans(kimai), bookedDays(kimai)
	}

	out := make([]ClassedRide, 0, len(rides))
	for _, r := range rides {
		c := ClassedRide{Ride: r, From: book.At(r.FromLat, r.FromLon), To: book.At(r.ToLat, r.ToLon), Class: ClassPrivate, Reason: ReasonRest}
		classify(&c, sheets, plugin, booked, base)
		out = append(out, c)
	}
	chain(out)
	return out
}

func classify(c *ClassedRide, sheets, plugin []span, booked map[[2]any]bool, base TravelBase) {
	if s, ok := bestCover(plugin, c.Start, c.End); ok {
		c.Class, c.Reason, c.CustomerID = s.class, ReasonPlugin, s.customer
		return
	}
	if s, ok := bestCover(sheets, c.Start, c.End); ok {
		c.Class, c.Reason, c.CustomerID = ClassBusiness, ReasonKimai, s.customer
		return
	}
	for _, site := range []*Site{c.To, c.From} {
		if site == nil || site.Kind != KindCustomer {
			continue
		}
		c.Class, c.Reason, c.CustomerID = ClassBusiness, ReasonCustomer, site.CustomerID
		c.Unconfirmed = booked != nil && !booked[[2]any{c.Day(), site.CustomerID}]
		return
	}
	if base == BaseWork && commutes(c.From, c.To) {
		c.Class, c.Reason = ClassCommute, ReasonCommute
	}
}

// commutes says whether a ride goes between home and work.
func commutes(from, to *Site) bool {
	if from == nil || to == nil {
		return false
	}
	return (from.Kind == KindHome && to.Kind == KindWork) || (from.Kind == KindWork && to.Kind == KindHome)
}

// chain makes a private ride between two business rides of its day
// business.
func chain(rides []ClassedRide) {
	for i := 1; i < len(rides)-1; i++ {
		r := &rides[i]
		if r.Reason != ReasonRest {
			continue
		}
		prev, next := rides[i-1], rides[i+1]
		if prev.Class != ClassBusiness || next.Class != ClassBusiness || !prev.Day().Equal(r.Day()) || !next.Day().Equal(r.Day()) {
			continue
		}
		r.Class, r.Reason, r.CustomerID = ClassBusiness, ReasonChain, next.CustomerID
	}
}
