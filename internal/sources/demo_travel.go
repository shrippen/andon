package sources

// Demo travel: Mara's tracks and the mileage plugin's places and trips,
// from the world's "location" and "logbook":
//
//	client day  home ─drive─► client site (visit) ─drive─► home
//	weekday     location.day      (studio, fish market, Speiche, beach)
//	Saturday    location.weekend  (Graufeld lighthouse, fish market by bus)

import (
	"math"
	"time"

	"andon/internal/sources/demoworld"
)

// Place kinds of the world and the mileage plugin's types for them.
var demoPlaceTypes = map[string]string{"home": "home", "office": "work", "customer": "customer"}

const (
	modeStationary = "stationary"
	modeDriving    = "driving"
	earthKM        = 6371.0
	metres         = 1000.0
)

func logbookOf(now time.Time) *demoLogbook {
	l := &demoLogbook{}
	demoworld.MustDecode("logbook", now, l)
	return l
}

// demoKM is the road distance between two world places.
func demoKM(a, b demoworld.Place, roadFactor float64) float64 {
	rad := math.Pi / 180
	h := math.Pow(math.Sin((b.Lat-a.Lat)*rad/2), 2) + math.Cos(a.Lat*rad)*math.Cos(b.Lat*rad)*math.Pow(math.Sin((b.Lon-a.Lon)*rad/2), 2)
	return 2 * earthKM * math.Asin(math.Sqrt(h)) * roadFactor
}

// demoSegment is one segment of a world day at d.
func demoSegment(d time.Time, from, to, a, b, mode string, roadFactor float64) DawarichSegment {
	p, q := demoWorld.Place(from), demoWorld.Place(to)
	ah, am := demoClock(a)
	bh, bm := demoClock(b)
	s := DawarichSegment{
		Start: time.Date(d.Year(), d.Month(), d.Day(), ah, am, 0, 0, time.UTC).Unix(),
		End:   time.Date(d.Year(), d.Month(), d.Day(), bh, bm, 0, 0, time.UTC).Unix(),
		Mode:  mode, FromLat: p.Lat, FromLon: p.Lon, ToLat: q.Lat, ToLon: q.Lon,
	}
	if mode != modeStationary {
		s.Meters = demoKM(p, q, roadFactor) * metres
	}
	return s
}

// demoTracks is one track a day from start to yesterday.
func demoTracks(now time.Time, start time.Time) []DawarichTrack {
	loc, logbook := locationOf(now), logbookOf(now)
	today := demoDay(now)
	visits := map[time.Time]bool{}
	days := clientDays(today)
	for _, i := range book.Time.ClientDays {
		if i < len(days) {
			visits[days[i]] = true
		}
	}
	home, site := demoWorld.Place(loc.Home), demoWorld.Place(loc.Site)
	drive := time.Duration(demoKM(home, site, logbook.RoadFactor)*logbook.MinutesPerKm) * time.Minute
	fromH, fromM := demoClock(loc.Visit.From)
	toH, toM := demoClock(loc.Visit.To)

	var out []DawarichTrack
	for d := demoDay(start); d.Before(today); d = d.AddDate(0, 0, 1) {
		var segs []DawarichSegment
		switch {
		case visits[d]:
			arrive := time.Date(d.Year(), d.Month(), d.Day(), fromH, fromM, 0, 0, time.UTC)
			leave := time.Date(d.Year(), d.Month(), d.Day(), toH, toM, 0, 0, time.UTC)
			there := demoSegment(d, loc.Home, loc.Site, "00:00", "00:00", modeDriving, logbook.RoadFactor)
			there.Start, there.End = arrive.Add(-drive).Unix(), arrive.Unix()
			back := demoSegment(d, loc.Site, loc.Home, "00:00", "00:00", modeDriving, logbook.RoadFactor)
			back.Start, back.End = leave.Unix(), leave.Add(drive).Unix()
			stay := demoSegment(d, loc.Site, loc.Site, "00:00", "00:00", modeStationary, logbook.RoadFactor)
			stay.Start, stay.End = arrive.Unix(), leave.Unix()
			segs = []DawarichSegment{there, stay, back}
		case int(d.Weekday()) == loc.Weekend.Weekday:
			for _, s := range loc.Weekend.Segments {
				segs = append(segs, demoSegment(d, s[0], s[1], s[2], s[3], s[4], logbook.RoadFactor))
			}
		case d.Weekday() != time.Saturday && d.Weekday() != time.Sunday:
			for _, s := range loc.Day.Segments {
				segs = append(segs, demoSegment(d, s[0], s[1], s[2], s[3], s[4], logbook.RoadFactor))
			}
		}
		if len(segs) == 0 {
			continue
		}
		out = append(out, DawarichTrack{ID: d.Unix(), Start: segs[0].Start, End: segs[len(segs)-1].End, Segments: segs})
	}
	return out
}

// demoCustomerID is a world customer's id in the demo Kimai, 0 if it has
// none there.
func demoCustomerID(world string) int64 {
	for i, id := range book.Customers {
		if id == world {
			return int64(i + 1)
		}
	}
	return 0
}

// demoKimaiPlaces are the mileage plugin's places: home, the studio and
// the client sites; those Dawarich knows as areas carry the area id.
func demoKimaiPlaces(now time.Time) []KimaiPlace {
	loc, logbook := locationOf(now), logbookOf(now)
	areas := map[string]int64{}
	for _, a := range loc.Areas {
		areas[a.Place] = a.ID
	}
	var out []KimaiPlace
	for _, p := range demoWorld.Places {
		kind, ok := demoPlaceTypes[p.Kind]
		if !ok || (kind == "home" && p.ID != loc.Home) {
			continue
		}
		out = append(out, KimaiPlace{ID: int64(len(out) + 1), Name: p.Name.DE(), Type: kind, CustomerID: demoCustomerID(p.Customer),
			Lat: p.Lat, Lon: p.Lon, Radius: logbook.PlaceRadius, AreaID: areas[p.ID]})
	}
	return out
}

// demoMileageTrips are the world's trips in the plugin, Mara's, leaving
// and coming back at the logbook's times.
func demoMileageTrips(now time.Time) []KimaiMileageTrip {
	var trips []demoTrip
	demoworld.MustDecode("trips", now, &trips)
	logbook, person := logbookOf(now), locationOf(now).Person
	today := demoDay(now)
	outH, outM := demoClock(logbook.Times.Out)
	var out []KimaiMileageTrip
	for i, t := range trips {
		if t.User != person {
			continue
		}
		d := today.AddDate(0, 0, t.Day)
		leave := time.Date(d.Year(), d.Month(), d.Day(), outH, outM, 0, 0, time.UTC)
		arrive := leave.Add(time.Duration(t.KM*logbook.MinutesPerKm) * time.Minute)
		out = append(out, KimaiMileageTrip{ID: int64(i + 1), Date: iso(d), Departure: leave.Format(time.RFC3339), Arrival: arrive.Format(time.RFC3339),
			Purpose: "business", KM: t.KM})
	}
	return out
}
