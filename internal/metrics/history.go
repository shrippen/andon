package metrics

// Key figure history: what the analysis stores each run and what rules
// read back as trends.
//
//	datasets ──Read──► Values {"truenas.pool.tank.used": 0.81, …} ──► table samples (daily)
//	                   Versions {"immich": "v1.132.3", …}      ──► change = event "update"
//	samples + events ──► History ──► rules (forecast, before/after)
//
// Recorders: see record.go.

import (
	"math"
	"sort"
	"strings"
	"time"

	"andon/internal/sources"
)

// HistoryDataset is the Env.Datasets key of the scope's History.
const HistoryDataset = "history"

// Point is one daily value.
type Point struct {
	Day   time.Time
	Value float64
}

// Event is one timeline entry (e.g. an update from one version to another).
type Event struct {
	At                    time.Time
	Kind, Subject, Detail string
}

// EventUpdate marks a version change.
const EventUpdate = "update"

// History is a scope's stored series and recent events.
type History struct {
	Series map[string][]Point
	Events []Event
}

// SeriesOf returns one series, oldest first; nil when unknown.
func (h *History) SeriesOf(key string) []Point {
	if h == nil {
		return nil
	}
	return h.Series[key]
}

// Keys returns the series keys with a prefix, sorted.
func (h *History) Keys(prefix string) []string {
	if h == nil {
		return nil
	}
	var out []string
	for k := range h.Series {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// Trend fits value = a + slope·days by least squares; ok is false with
// fewer than min points.
func Trend(points []Point, min int) (slopePerDay, last float64, ok bool) {
	if len(points) < min || len(points) < 2 {
		return 0, 0, false
	}
	origin := points[0].Day
	var sx, sy, sxx, sxy float64
	n := float64(len(points))
	for _, p := range points {
		x := p.Day.Sub(origin).Hours() / hoursPerDay
		sx, sy, sxx, sxy = sx+x, sy+p.Value, sxx+x*x, sxy+x*p.Value
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return 0, points[len(points)-1].Value, false
	}
	return (n*sxy - sx*sy) / den, points[len(points)-1].Value, true
}

// SlopeError is the standard error of Trend's slope: how far the daily
// pace may be off given how the points scatter around the line. ok is
// false with fewer than three points.
func SlopeError(points []Point) (float64, bool) {
	n := float64(len(points))
	if len(points) < 3 {
		return 0, false
	}
	origin := points[0].Day
	xs := make([]float64, len(points))
	var sx, sy float64
	for i, p := range points {
		xs[i] = p.Day.Sub(origin).Hours() / hoursPerDay
		sx, sy = sx+xs[i], sy+p.Value
	}
	mx, my := sx/n, sy/n
	var sxx, sxy float64
	for i, p := range points {
		sxx, sxy = sxx+(xs[i]-mx)*(xs[i]-mx), sxy+(xs[i]-mx)*(p.Value-my)
	}
	if sxx == 0 {
		return 0, false
	}
	slope := sxy / sxx
	var rss float64
	for i, p := range points {
		r := p.Value - (my + slope*(xs[i]-mx))
		rss += r * r
	}
	return math.Sqrt(rss / (n - 2) / sxx), true
}

// Typical is the mean or median of the points within [from, to), and
// how many there were.
func Typical(points []Point, from, to time.Time, center Center) (float64, int) {
	var values []float64
	for _, p := range points {
		if !p.Day.Before(from) && p.Day.Before(to) {
			values = append(values, p.Value)
		}
	}
	return center.Of(values), len(values)
}

// ValueOn returns the newest point on or before day.
func ValueOn(points []Point, day time.Time) (float64, bool) {
	found, ok := 0.0, false
	for _, p := range points {
		if p.Day.After(day) {
			break
		}
		found, ok = p.Value, true
	}
	return found, ok
}

// key joins parts of a series key, keeping dots out of names.
func key(parts ...string) string {
	for i, p := range parts {
		parts[i] = strings.ReplaceAll(strings.ToLower(strings.TrimSpace(p)), ".", "_")
	}
	return strings.Join(parts, ".")
}

// SampleKey builds a series key from parts ("kuma", "ms", "NAS.lan") → "kuma.ms.nas_lan".
func SampleKey(parts ...string) string { return key(parts...) }

// The history of the services without a file of their own: daily
// figures and running versions.
func init() {
	Record(func(d *sources.TrueNASDataset, _ time.Time, r *Readings) {
		for _, p := range d.Pools {
			if p.Size > 0 {
				r.Set(key("truenas", "pool", p.Name, "used"), p.Allocated/p.Size)
			}
		}
		r.Version("TrueNAS", d.Version)
	})
	Record(func(d *sources.ProxmoxDataset, _ time.Time, r *Readings) {
		for _, n := range d.Nodes {
			for _, s := range n.Storages {
				if s.Total > 0 {
					r.Set(key("proxmox", "storage", n.Name+"/"+s.Name, "used"), s.Used/s.Total)
				}
			}
		}
	})
	Record(func(d *sources.ScrutinyDataset, _ time.Time, r *Readings) {
		for _, disk := range d.Disks {
			if disk.Temp > 0 {
				r.Set(key("scrutiny", "temp", disk.Name), disk.Temp)
			}
		}
	})
	Record(func(d *sources.BorgDataset, _ time.Time, r *Readings) {
		if d.TotalBytes > 0 {
			r.Set(key("borg", "used"), d.UsedBytes/d.TotalBytes)
		}
		// A client's repository that jumps or stops growing shows a
		// backup that took far more, or nothing at all.
		for _, c := range d.Clients {
			if c.RepoBytes > 0 {
				r.Set(key("borg", "size", c.Name), c.RepoBytes)
			}
		}
	})
	Record(func(d *sources.ImmichDataset, _ time.Time, r *Readings) {
		r.Set(key("immich", "items"), float64(d.Photos+d.Videos))
		if d.DiskPercent > 0 {
			r.Set(key("immich", "disk", "used"), d.DiskPercent/percentScale)
		}
		r.Version("Immich", d.Version)
	})
	Record(func(d *sources.NextcloudDataset, _ time.Time, r *Readings) {
		r.Set(key("nextcloud", "files"), float64(d.Files))
		r.Version("Nextcloud", d.Version)
	})
	// A certificate's end as a day number: when it jumps forward, it was
	// renewed.
	Record(func(d *sources.CertDataset, _ time.Time, r *Readings) {
		for _, c := range d.Certs {
			if !c.NotAfter.IsZero() {
				r.Set(key("certs", "until", c.Host), float64(c.NotAfter.Unix()/secondsPerDay))
			}
		}
	})
	Record(func(d *sources.SpeedtestDataset, _ time.Time, r *Readings) {
		if !d.At.IsZero() {
			r.Set(key("speedtest", "down"), d.Down)
			r.Set(key("speedtest", "up"), d.Up)
		}
	})
	Record(func(d *sources.SureDataset, _ time.Time, r *Readings) { r.Set(key("sure", "cash"), SureCash(d)) })
	Record(func(d *sources.KumaDataset, _ time.Time, r *Readings) {
		for _, m := range d.Monitors {
			if m.MS > 0 {
				r.Set(key("kuma", "ms", m.Name), m.MS)
			}
		}
	})
	Record(func(d *sources.DNSFilterDataset, _ time.Time, r *Readings) {
		for _, c := range d.TopClients {
			r.Set(key("dns", "q", c.IP), float64(c.Queries))
		}
	})
	Record(func(d *sources.PaperlessDataset, _ time.Time, r *Readings) {
		if d.Total >= 0 {
			r.Set(key("paperless", "docs"), float64(d.Total))
		}
	})
	Record(func(d *sources.AuthentikDataset, _ time.Time, r *Readings) {
		// One marker per user and country a login came from.
		for _, l := range d.Logins {
			if l.Country != "" {
				r.Set(key("authentik", "country", l.User, l.Country), 1)
			}
		}
		r.Version("authentik", d.Version)
	})
	Record(func(d *sources.MediaServerDataset, _ time.Time, r *Readings) { r.Version(d.Kind, d.Version) })
	// Kintsugi's share of suggestions taken up, once decided.
	Record(func(d *sources.KintsugiDataset, _ time.Time, r *Readings) {
		if d.Rate >= 0 {
			r.Set(key("kintsugi", "rate"), float64(d.Rate))
		}
	})
	// The tunnel's state and exit: a drop or a new exit is on the timeline.
	Record(func(d *sources.GluetunDataset, _ time.Time, r *Readings) {
		r.State(SubjectVPN, d.Status)
		r.State(SubjectVPNExit, strings.TrimSpace(d.ExitIP+" "+d.Country))
	})
	Record(func(d *sources.ArrDataset, _ time.Time, r *Readings) { r.Version(d.App, d.Version) })
	Record(func(d *sources.GatewayDataset, _ time.Time, r *Readings) { r.Version(d.Kind, d.Version) })
	Record(func(d *sources.VaultwardenDataset, _ time.Time, r *Readings) { r.Version("Vaultwarden", d.Version) })
	Record(func(d *sources.KomodoDataset, _ time.Time, r *Readings) {
		// A stack whose pending image updates disappear was redeployed.
		for _, s := range d.Stacks {
			r.Versions[stackPrefix+s.Name] = pendingPrefix + strings.Join(s.Updates, ",")
		}
	})
}

const (
	// pendingPrefix marks a Komodo stack's list of services with newer images.
	pendingPrefix = "pending:"
	// stackPrefix keeps a stack apart from a service of the same name.
	stackPrefix = "stack:"
)

// VersionEvent turns a version change into a timeline event; a first
// sighting or a newly announced image is none.
func VersionEvent(subject, old, now string, at time.Time) (Event, bool) {
	if old == "" || old == now {
		return Event{}, false
	}
	if strings.HasPrefix(old, pendingPrefix) {
		if now != pendingPrefix || old == pendingPrefix {
			return Event{}, false
		}
		return Event{At: at, Kind: EventUpdate, Subject: strings.TrimPrefix(subject, stackPrefix), Detail: strings.TrimPrefix(old, pendingPrefix)}, true
	}
	return Event{At: at, Kind: EventUpdate, Subject: subject, Detail: old + " → " + now}, true
}

// secondsPerDay turns Unix times into day numbers.
const secondsPerDay = 24 * 60 * 60

// LastRenewal is the day a certificate's stored end last moved forward;
// zero when the history never saw it change.
func LastRenewal(h *History, host string) time.Time {
	points := h.SeriesOf(key("certs", "until", host))
	var last time.Time
	for i := 1; i < len(points); i++ {
		if points[i].Value > points[i-1].Value {
			last = points[i].Day
		}
	}
	return last
}
