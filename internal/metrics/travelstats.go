// Travel: the classified rides of a Dawarich connection and what follows
// from them (allowances, sums per customer, month and class, places
// rides end at without a site).
package metrics

import (
	"math"
	"sort"
	"strings"
	"time"

	"andon/internal/sources"
)

// TravelSettings are a space's "travel" settings.
type TravelSettings struct {
	Base TravelBase
	// KMRate is the flat rate per business km (€).
	KMRate float64
	// CompanyCar: the car is a business asset (1 % rule needs > 50 %
	// business use).
	CompanyCar bool
	// FuelWords are the Sure categories or merchants of fuel and charging.
	FuelWords []string
}

// Default travel settings (German rates).
const (
	DefaultKMRate     = 0.30
	defaultFuelWords  = "Tanken, Kraftstoff, Fuel, Laden"
	AllowancePartial  = 14.0 // €, > 8 h away or a day of arrival/departure
	AllowanceFull     = 28.0 // €, a full day away
	allowanceMinHours = 8.0
)

// TravelSettingsOf reads settings["travel"].
func TravelSettingsOf(settings map[string]any) TravelSettings {
	raw, _ := settings["travel"].(map[string]any)
	out := TravelSettings{Base: BaseHome, KMRate: DefaultKMRate}
	if base, _ := raw["base"].(string); base == string(BaseWork) {
		out.Base = BaseWork
	}
	// The rate used to be a setting of the rule geo.travel_costs.
	rules, _ := settings["rules"].(map[string]any)
	old, _ := rules["geo.travel_costs"].(map[string]any)
	for _, m := range []map[string]any{old, raw} {
		if rate, ok := m["km_rate"].(float64); ok && rate > 0 {
			out.KMRate = rate
		}
	}
	out.CompanyCar, _ = raw["company_car"].(bool)
	words, ok := raw["fuel_words"].(string)
	if !ok {
		words = defaultFuelWords
	}
	for _, w := range strings.Split(words, ",") {
		if w = strings.ToLower(strings.TrimSpace(w)); w != "" {
			out.FuelWords = append(out.FuelWords, w)
		}
	}
	return out
}

// Travel is a Dawarich connection's rides, classified.
type Travel struct {
	Rides     []ClassedRide
	Book      *Book
	Estimated bool // from visits: Dawarich has no tracks
	Partial   bool // more tracks are still being read
}

// TravelOf classifies the rides of geo; kimai may be nil.
func TravelOf(geo *sources.DawarichDataset, kimai *sources.KimaiDataset, options map[string]any, set TravelSettings, now time.Time) Travel {
	book := BookOf(geo, kimai, options)
	t := Travel{Book: book, Partial: geo.TracksState == sources.TracksPartial}
	rides := Rides(geo)
	if !HasTracks(geo) {
		rides, t.Estimated = EstimatedRides(geo, book), true
	}
	t.Rides = Classify(rides, book, kimai, set.Base, now)
	return t
}

// Between is the rides starting in [start, end] (days, inclusive).
func (t Travel) Between(start, end time.Time) []ClassedRide {
	var out []ClassedRide
	for _, r := range t.Rides {
		d := r.Day()
		if d.Before(start) || d.After(end) {
			continue
		}
		out = append(out, r)
	}
	return out
}

// ── sums ──

// RideSum is km, minutes and count of some rides.
type RideSum struct {
	KM      float64
	Minutes float64
	Rides   int
}

func (s *RideSum) add(r ClassedRide) {
	s.KM += r.KM
	s.Minutes += r.Minutes()
	s.Rides++
}

// ByClass sums rides per class.
func ByClass(rides []ClassedRide) map[RideClass]RideSum {
	out := map[RideClass]RideSum{}
	for _, r := range rides {
		s := out[r.Class]
		s.add(r)
		out[r.Class] = s
	}
	return out
}

// ByCustomer sums business rides per customer (0 = unknown).
func ByCustomer(rides []ClassedRide) map[int64]RideSum {
	out := map[int64]RideSum{}
	for _, r := range rides {
		if r.Class != ClassBusiness {
			continue
		}
		s := out[r.CustomerID]
		s.add(r)
		out[r.CustomerID] = s
	}
	return out
}

// ByMode sums rides per transportation mode.
func ByMode(rides []ClassedRide) map[string]RideSum {
	out := map[string]RideSum{}
	for _, r := range rides {
		s := out[r.Mode]
		s.add(r)
		out[r.Mode] = s
	}
	return out
}

// MonthKM is km per month (1–12) and class in year.
func MonthKM(rides []ClassedRide, year int) [12]map[RideClass]float64 {
	var out [12]map[RideClass]float64
	for i := range out {
		out[i] = map[RideClass]float64{}
	}
	for _, r := range rides {
		d := r.Day()
		if d.Year() != year {
			continue
		}
		out[d.Month()-1][r.Class] += r.KM
	}
	return out
}

// WeekMinutes is the minutes on the move per ISO week, oldest first.
type WeekMinutes struct {
	Week    time.Time // Monday
	Minutes map[RideClass]float64
}

// Weeks sums ride minutes per week and class.
func Weeks(rides []ClassedRide) []WeekMinutes {
	byWeek := map[time.Time]map[RideClass]float64{}
	for _, r := range rides {
		d := r.Day()
		monday := d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
		if byWeek[monday] == nil {
			byWeek[monday] = map[RideClass]float64{}
		}
		byWeek[monday][r.Class] += r.Minutes()
	}
	out := make([]WeekMinutes, 0, len(byWeek))
	for w, m := range byWeek {
		out = append(out, WeekMinutes{Week: w, Minutes: m})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Week.Before(out[j].Week) })
	return out
}

// Heat is the minutes on the move per weekday (0 = Monday) and hour.
func Heat(rides []ClassedRide) [7][24]float64 {
	var out [7][24]float64
	for _, r := range rides {
		for t := r.Start.In(time.Local); t.Before(r.End); {
			next := t.Truncate(time.Hour).Add(time.Hour)
			if next.After(r.End) {
				next = r.End
			}
			out[(int(t.Weekday())+6)%7][t.Hour()] += next.Sub(t).Minutes()
			t = next
		}
	}
	return out
}

// ── destinations ──

// Destination is where rides end: a site, or an unknown spot.
type Destination struct {
	Site     *Site // nil: no site there
	Lat, Lon float64
	Rides    int
	KM       float64
	Last     time.Time
}

// spotDigits rounds unknown ends to about 100 m to group them.
const spotDigits = 3

// Destinations groups rides by where they end, most rides first.
func Destinations(rides []ClassedRide) []Destination {
	type key struct {
		site     string
		lat, lon float64
	}
	byKey := map[key]*Destination{}
	var order []key
	scale := math.Pow(10, spotDigits)
	for _, r := range rides {
		k := key{}
		if r.To != nil {
			k.site = r.To.Key
		} else {
			k.lat, k.lon = math.Round(r.ToLat*scale)/scale, math.Round(r.ToLon*scale)/scale
		}
		d := byKey[k]
		if d == nil {
			d = &Destination{Site: r.To, Lat: k.lat, Lon: k.lon}
			if r.To != nil {
				d.Lat, d.Lon = r.To.Lat, r.To.Lon
			}
			byKey[k] = d
			order = append(order, k)
		}
		d.Rides++
		d.KM += r.KM
		if r.End.After(d.Last) {
			d.Last = r.End
		}
	}
	out := make([]Destination, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Rides > out[j].Rides })
	return out
}

// Unplaced is the destinations without an assigned site (none there, or
// a site nobody gave a kind), at least min rides each.
func Unplaced(rides []ClassedRide, min int) []Destination {
	var out []Destination
	for _, d := range Destinations(rides) {
		if d.Rides < min || (d.Site != nil && d.Site.Kind != KindNone) {
			continue
		}
		out = append(out, d)
	}
	return out
}

// ── allowances ──

// AllowanceDay is one day with a meal allowance.
type AllowanceDay struct {
	Day  time.Time
	Full bool // a full day away (AllowanceFull)
}

// Amount is the day's allowance.
func (a AllowanceDay) Amount() float64 {
	if a.Full {
		return AllowanceFull
	}
	return AllowancePartial
}

// allowanceMaxDays drops an absence without a ride back home (tracking
// stopped) instead of counting weeks.
const allowanceMaxDays = 14

// Allowances are the meal allowance days: an absence from home (and the
// place of work, with base work) that contains a business ride. A day
// trip counts above 8 h; a journey over night counts its first and last
// day partly and the days between fully.
func Allowances(rides []ClassedRide, base TravelBase) []AllowanceDay {
	isBase := func(s *Site) bool {
		return s != nil && (s.Kind == KindHome || (base == BaseWork && s.Kind == KindWork))
	}

	var out []AllowanceDay
	var leftAt time.Time
	business := false
	for _, r := range rides {
		if isBase(r.From) {
			leftAt, business = r.Start, false
		}
		if leftAt.IsZero() {
			continue
		}
		business = business || r.Class == ClassBusiness
		if !isBase(r.To) {
			continue
		}
		if business {
			out = append(out, absenceDays(leftAt, r.End)...)
		}
		leftAt = time.Time{}
	}
	return out
}

// absenceDays turns one absence into allowance days.
func absenceDays(from, to time.Time) []AllowanceDay {
	from, to = from.In(time.Local), to.In(time.Local)
	first := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	last := time.Date(to.Year(), to.Month(), to.Day(), 0, 0, 0, 0, time.UTC)
	days := int(last.Sub(first).Hours() / 24)
	if days > allowanceMaxDays {
		return nil
	}
	if days == 0 {
		if to.Sub(from).Hours() <= allowanceMinHours {
			return nil
		}
		return []AllowanceDay{{Day: first}}
	}
	out := []AllowanceDay{{Day: first}}
	for i := 1; i < days; i++ {
		out = append(out, AllowanceDay{Day: first.AddDate(0, 0, i), Full: true})
	}
	return append(out, AllowanceDay{Day: last})
}

// ── commute, car, fuel ──

// CommuteDays is the days with a commute and the one-way distance (the
// shortest commute ride).
func CommuteDays(rides []ClassedRide) (int, float64) {
	days := map[time.Time]bool{}
	shortest := 0.0
	for _, r := range rides {
		if r.Class != ClassCommute {
			continue
		}
		days[r.Day()] = true
		if shortest == 0 || r.KM < shortest {
			shortest = r.KM
		}
	}
	return len(days), shortest
}

// CarShareLimit is the private share of a business car above which the
// 1 % method is not allowed.
const CarShareLimit = 0.5

// CarShare is the private share of the km driven by car (0–1), and the
// km driven; 0, 0 without car rides.
func CarShare(rides []ClassedRide) (float64, float64) {
	var private, total float64
	for _, r := range rides {
		if r.Mode != ModeDriving {
			continue
		}
		total += r.KM
		if r.Class == ClassPrivate {
			private += r.KM
		}
	}
	if total == 0 {
		return 0, 0
	}
	return private / total, total
}

// FuelSpent is what Sure booked for fuel or charging since from: the
// transactions whose category or merchant holds one of the words.
func FuelSpent(sure *sources.SureDataset, words []string, from time.Time) float64 {
	var sum float64
	for _, t := range sure.Transactions {
		day, ok := ParseDay(t.Date)
		if !ok || day.Before(from) {
			continue
		}
		text := strings.ToLower(t.Category + " " + t.Merchant)
		for _, w := range words {
			if strings.Contains(text, w) {
				sum += math.Abs(t.Amount)
				break
			}
		}
	}
	return sum
}
