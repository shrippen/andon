// Classify: each ride is business, commute or private, with the reason.
// The first rule that matches wins:
//
//	0 manual    the user set the class by hand              → that class
//	1 plugin    a Kimai mileage trip covers half the ride   → its purpose
//	2 kimai     half the ride lies in booked Kimai time     → business
//	3 customer  it starts or ends at a customer site        → business
//	4 commute   home ↔ work (only with a place of work)     → commute
//	5 rest                                                  → private
//
// Afterwards a private ride between two business rides of the same day
// becomes business (reason chain): customer A → bakery → customer B.
// For the rest rides a learned model may suggest a class (ridelearn.go);
// a suggestion changes nothing until the user sets the class by hand.
package metrics

import (
	"slices"
	"sort"
	"strconv"
	"time"

	"andon/internal/sources"
)

// RideClass is what a ride was for.
type RideClass string

const (
	ClassPrivate  RideClass = "private"
	ClassBusiness RideClass = "business"
	ClassCommute  RideClass = "commute"
	// ClassAuto is no class set by hand: the rules decide.
	ClassAuto RideClass = ""
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
	ReasonManual   RideReason = "manual"
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

// OptionRides is the Dawarich connection's option of classes set by hand
// without the plugin: {"1767600000": "private"} (ride start, Unix s).
const OptionRides = "rides"

// Overrides are classes set by hand, by ride start (Unix seconds).
type Overrides map[int64]RideClass

// ParseOverrides reads OptionRides; unknown classes and keys are left out.
func ParseOverrides(options map[string]any) Overrides {
	raw, _ := options[OptionRides].(map[string]any)
	out := Overrides{}
	for key, v := range raw {
		start, err := strconv.ParseInt(key, 10, 64)
		class, _ := v.(string)
		if err != nil || !slices.Contains(RideClasses, RideClass(class)) {
			continue
		}
		out[start] = RideClass(class)
	}
	return out
}

// ClassedRide is a ride with its sites and class. CustomerID is 0 when
// unknown; Unconfirmed marks a customer-site ride without Kimai time for
// that customer on its day. Trip is the covering plugin trip, 0 if none.
type ClassedRide struct {
	Ride
	From, To    *Site
	Class       RideClass
	Reason      RideReason
	CustomerID  int64
	Unconfirmed bool
	Trip        int64
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
	trip     int64
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

// covers holds spans sorted by start, so a ride looks only at those
// near it, not at all of them (2 M pairs for the demo's rides):
//
//	spans by from:  [a──)  [b────)   [c─)     [d──)
//	ride [x, y):              [x─────y)
//	                 ends ≤ x │ checked  │ from ≥ y
//
// reach[i] is the latest end among spans[:i+1]; once it is ≤ x, no
// earlier span reaches the ride.
type covers struct {
	spans []span
	order []int // index in the given slice: ties go to the first given
	reach []time.Time
}

func coversOf(spans []span) covers {
	order := make([]int, len(spans))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(i, j int) bool { return spans[order[i]].from.Before(spans[order[j]].from) })

	c := covers{spans: make([]span, len(spans)), order: order, reach: make([]time.Time, len(spans))}
	for i, k := range order {
		c.spans[i] = spans[k]
		c.reach[i] = spans[k].to
		if i > 0 && c.reach[i-1].After(c.reach[i]) {
			c.reach[i] = c.reach[i-1]
		}
	}
	return c
}

// best is the span covering most of [a, b) if all spans together cover
// at least coverShare of it; spans are assumed not to overlap each
// other much.
func (c covers) best(a, b time.Time) (span, bool) {
	total := b.Sub(a)
	if total <= 0 {
		return span{}, false
	}
	end := sort.Search(len(c.spans), func(i int) bool { return !c.spans[i].from.Before(b) })

	bestAt := -1
	var bestD, sum time.Duration
	for i := end - 1; i >= 0 && c.reach[i].After(a); i-- {
		d := c.spans[i].overlap(a, b)
		sum += d
		if d > bestD || (d == bestD && d > 0 && c.order[i] < c.order[bestAt]) {
			bestAt, bestD = i, d
		}
	}
	if bestAt < 0 || float64(sum) < coverShare*float64(total) {
		return span{}, false
	}
	return c.spans[bestAt], true
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
		out = append(out, span{from: from, to: to, customer: customers[t.Project], class: class, trip: t.ID})
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
// nil (rules 1 and 2 then do not apply), manual too.
func Classify(rides []Ride, book *Book, kimai *sources.KimaiDataset, base TravelBase, manual Overrides, now time.Time) []ClassedRide {
	var sheets, plugin covers
	var booked map[[2]any]bool
	if kimai != nil {
		sheets, plugin, booked = coversOf(sheetSpans(kimai, now)), coversOf(pluginSpans(kimai)), bookedDays(kimai)
	}

	out := make([]ClassedRide, 0, len(rides))
	for _, r := range rides {
		c := ClassedRide{Ride: r, From: book.At(r.FromLat, r.FromLon), To: book.At(r.ToLat, r.ToLon), Class: ClassPrivate, Reason: ReasonRest}
		classify(&c, sheets, plugin, booked, base)
		if class, ok := manual[r.Start.Unix()]; ok {
			byHand(&c, class)
		}
		out = append(out, c)
	}
	chain(out)
	return out
}

func classify(c *ClassedRide, sheets, plugin covers, booked map[[2]any]bool, base TravelBase) {
	if s, ok := plugin.best(c.Start, c.End); ok {
		c.Class, c.Reason, c.CustomerID, c.Trip = s.class, ReasonPlugin, s.customer, s.trip
		return
	}
	if s, ok := sheets.best(c.Start, c.End); ok {
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

// byHand gives a classified ride the class set by hand; a business ride
// keeps the customer the rules found, others have none.
func byHand(c *ClassedRide, class RideClass) {
	if class != ClassBusiness {
		c.CustomerID = 0
	}
	c.Class, c.Reason, c.Unconfirmed = class, ReasonManual, false
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
