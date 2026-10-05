// Rides: trips as Dawarich tracked them. A track is split into rides by
// its transportation-mode segments:
//
//	segments  ▬drive▬ ·stop 2m· ▬drive▬ ··stop 40m·· ~walk~ ▬bus▬
//	rides     └──── ride 1 (car) ────┘                └w┘  └bus┘
//
// A ride is a run of moving segments of one kind (motorized or on foot /
// by bike); a stop of at least rideStop, a recording gap as long or a
// change of kind ends it. Short stops (traffic lights) stay in the ride.
package metrics

import (
	"time"

	"andon/internal/sources"
)

const (
	rideStop = 5 * time.Minute
	// rideMinKM drops GPS jitter that Dawarich reports as movement.
	rideMinKM = 0.2
	// activeKMH is the speed above which an unclassified segment counts
	// as motorized.
	activeKMH   = 20.0
	metresPerKM = 1000.0
)

// Transportation modes of Dawarich segments.
const (
	ModeStationary = "stationary"
	ModeUnknown    = "unknown"
	ModeDriving    = "driving"
	ModeWalking    = "walking"
	ModeRunning    = "running"
	ModeCycling    = "cycling"
	ModeMotorcycle = "motorcycle"
)

// activeModes move by muscle; everything else that moves is motorized.
var activeModes = map[string]bool{ModeWalking: true, ModeRunning: true, ModeCycling: true}

// Ride is one trip from a stop to the next. Mode is the mode with the
// longest distance; Estimated rides come from visits (no tracks).
type Ride struct {
	Start, End       time.Time
	KM               float64
	Mode             string
	Motorized        bool
	FromLat, FromLon float64
	ToLat, ToLon     float64
	Estimated        bool
}

// Minutes is the ride's duration.
func (r Ride) Minutes() float64 { return r.End.Sub(r.Start).Minutes() }

// Rides splits the dataset's tracks into rides, oldest first.
func Rides(data *sources.DawarichDataset) []Ride {
	var out []Ride
	for _, t := range data.Tracks {
		out = append(out, trackRides(t)...)
	}
	return out
}

// segMoving says whether a segment moves, and if so whether motorized.
func segMoving(s sources.DawarichSegment) (moving, motorized bool) {
	if s.Mode == ModeStationary || s.Meters <= 0 {
		return false, false
	}
	if s.Mode != ModeUnknown {
		return true, !activeModes[s.Mode]
	}
	hours := float64(s.End-s.Start) / float64(time.Hour/time.Second)
	if hours <= 0 {
		return false, false
	}
	return true, s.Meters/metresPerKM/hours >= activeKMH
}

// rideBuilder collects the segments of the ride in progress.
type rideBuilder struct {
	ride   Ride
	open   bool
	byMode map[string]float64
}

func (b *rideBuilder) add(s sources.DawarichSegment, motorized bool) {
	if !b.open {
		b.ride = Ride{Start: time.Unix(s.Start, 0), Motorized: motorized, FromLat: s.FromLat, FromLon: s.FromLon}
		b.byMode = map[string]float64{}
		b.open = true
	}
	b.ride.End = time.Unix(s.End, 0)
	b.ride.KM += s.Meters / metresPerKM
	b.ride.ToLat, b.ride.ToLon = s.ToLat, s.ToLon
	b.byMode[s.Mode] += s.Meters
}

// close ends the ride in progress; it is kept unless it is jitter.
func (b *rideBuilder) close(out []Ride) []Ride {
	if !b.open {
		return out
	}
	b.open = false
	if b.ride.KM < rideMinKM {
		return out
	}
	best := 0.0
	for mode, m := range b.byMode {
		if m > best || (m == best && mode < b.ride.Mode) {
			best, b.ride.Mode = m, mode
		}
	}
	b.ride.KM = round1(b.ride.KM)
	return append(out, b.ride)
}

func trackRides(t sources.DawarichTrack) []Ride {
	var out []Ride
	var b rideBuilder
	var lastEnd int64
	for i, s := range t.Segments {
		gap := i > 0 && time.Duration(s.Start-lastEnd)*time.Second >= rideStop
		lastEnd = s.End

		moving, motorized := segMoving(s)
		if !moving {
			// a long stop ends the ride, a short one stays part of it
			if time.Duration(s.End-s.Start)*time.Second >= rideStop {
				out = b.close(out)
			}
			continue
		}
		if gap || (b.open && b.ride.Motorized != motorized) {
			out = b.close(out)
		}
		b.add(s, motorized)
	}
	return b.close(out)
}

// estimatedKMH turns an estimated distance into a driving time.
const estimatedKMH = 50.0

// EstimatedRides stands in for tracks a Dawarich without them lacks: to
// each client visit and back from home, straight line × road factor.
func EstimatedRides(data *sources.DawarichDataset, book *Book) []Ride {
	home := book.Home()
	if home == nil {
		return nil
	}
	var out []Ride
	for _, v := range data.Visits {
		site := book.visitSite(v, data.Areas)
		if site == nil || site.Kind != KindCustomer {
			continue
		}
		start, ok1 := ParseTime(v.Start)
		end, ok2 := ParseTime(v.End)
		if !ok1 || !ok2 {
			continue
		}
		km := round1(DistanceKM(home.Lat, home.Lon, site.Lat, site.Lon) * roadFactor)
		drive := time.Duration(km / estimatedKMH * float64(time.Hour))
		out = append(out,
			Ride{Start: start.Add(-drive), End: start, KM: km, Mode: ModeDriving, Motorized: true, FromLat: home.Lat, FromLon: home.Lon, ToLat: site.Lat, ToLon: site.Lon, Estimated: true},
			Ride{Start: end, End: end.Add(drive), KM: km, Mode: ModeDriving, Motorized: true, FromLat: site.Lat, FromLon: site.Lon, ToLat: home.Lat, ToLon: home.Lon, Estimated: true})
	}
	return out
}

// HasTracks says whether the rides come from tracks (else estimated).
func HasTracks(data *sources.DawarichDataset) bool {
	return data.TracksState == sources.TracksOK || data.TracksState == sources.TracksPartial
}
