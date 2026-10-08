package metrics

// Reconnects of the router's line (FRITZ!Box over TR-064). The box tells
// how long the connection has been up; each run records when it came up
// as the state "WAN", or "–" while it is down. A new time is a reconnect:
//
//	run 08:40  "2026-09-12 04:02 UTC" → "–"                   (line down)
//	run 09:15  "–" → "2026-09-20 09:12 UTC"   ⇒ reconnect 09:12, down ≥ 32 min
//	run 04:05  "2026-09-11 04:02 UTC" → "2026-09-12 04:02 UTC" ⇒ reconnect 04:02, short
//
// Downtime is known only when a run saw the line down; it lasted at
// least from that run to the new connection.

import (
	"sort"
	"strings"
	"time"

	"andon/internal/sources"
)

const (
	// WANDown is the line's state while it is down.
	WANDown = "–"
	// wanLayout is a connection's start as the state stores it.
	wanLayout = "2006-01-02 15:04 UTC"
	// connectJitter: starts this close are the same connection (runs
	// compute it from the uptime a few seconds apart).
	connectJitter = 2 * time.Minute
	changeArrow   = " → "
)

// Reconnect is one new connection of the line.
type Reconnect struct {
	At   time.Time     // when the line came up again
	Seen bool          // a run saw the line down before
	Down time.Duration // then: at least this long; else short (between two runs)
}

// sameConnect reports two states that name the same connection.
func sameConnect(a, b string) bool {
	x, err1 := time.Parse(wanLayout, a)
	y, err2 := time.Parse(wanLayout, b)
	if err1 != nil || err2 != nil {
		return false
	}
	d := x.Sub(y)
	return d < connectJitter && d > -connectJitter
}

// Reconnects are the line's new connections since a time, oldest first.
func Reconnects(h *History, since time.Time) []Reconnect {
	if h == nil {
		return nil
	}
	var events []Event
	for _, e := range h.Events {
		if e.Kind == EventChange && e.Subject == SubjectWAN {
			events = append(events, e)
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })

	var out []Reconnect
	var downAt time.Time // when a run last saw the line down
	for _, e := range events {
		old, now, _ := strings.Cut(e.Detail, changeArrow)
		if now == WANDown {
			downAt = e.At
			continue
		}
		up, err := time.Parse(wanLayout, now)
		if err != nil || up.Before(since) {
			continue
		}
		r := Reconnect{At: up}
		if old == WANDown && !downAt.IsZero() {
			r.Seen, r.Down = true, max(up.Sub(downAt), 0)
		}
		out = append(out, r)
		downAt = time.Time{}
	}
	return out
}

func init() {
	Record(func(d *sources.FritzDataset, _ time.Time, r *Readings) {
		switch {
		case !d.Connected():
			r.State(SubjectWAN, WANDown)
		case !d.Since.IsZero():
			r.State(SubjectWAN, d.Since.UTC().Format(wanLayout))
		}

		// Devices online, as the router's leases: the first mark of a
		// name is the day it joined (gateway.new_device). Guests come
		// and go; they are left out.
		for _, h := range d.Hosts {
			if h.Active && !h.Guest {
				r.Set(key("fritz", "seen", h.Name), 1)
			}
		}
	})
}
