package connections

// Connection health and token hygiene:
//
//	health   last success, failure rate, mean response time (7 days)
//	hygiene  token expiry date and a daily fetch budget, set by managers

import (
	"database/sql"
	"errors"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/repos/content"
	data "andon/internal/repos/data"
	"andon/internal/services/access"
	"andon/internal/services/audit"
	"andon/internal/services/svcdata"
)

// healthDays is the window of the health view.
const healthDays = 7

// ErrBadDate means the expiry is no "2026-12-31" date.
var ErrBadDate = errors.New("connection.bad_date")

// Health is one connection's recent fetch record.
type Health struct {
	Fetches   int
	FailPct   int
	AvgMS     int
	LastOK    time.Time // zero = none in the window
	LastFail  time.Time // zero = none in the window
	LastError string
	Today     int // fetches today, for the budget
}

// HealthState is how a connection stands now.
type HealthState string

const (
	HealthUnknown HealthState = "unknown" // no fetch in the window
	HealthOK      HealthState = "ok"
	HealthShaky   HealthState = "shaky"   // works now, failed recently
	HealthFailing HealthState = "failing" // the last fetch failed
)

// shakyPct: below this failure rate single failures are noise (1 of 33
// fetches is 3 %).
const shakyPct = 5

// State follows the latest fetch: a successful test ends "failing" at
// once, the failure rate only makes it "shaky".
func (h Health) State() HealthState {
	switch {
	case h.Fetches == 0:
		return HealthUnknown
	case !h.LastFail.IsZero() && (h.LastOK.IsZero() || h.LastFail.After(h.LastOK)):
		return HealthFailing
	case h.FailPct >= shakyPct:
		return HealthShaky
	}
	return HealthOK
}

// ErrorSettled tells that the last error is past: a success came after it.
func (h Health) ErrorSettled() bool {
	return !h.LastFail.IsZero() && h.LastOK.After(h.LastFail)
}

func healthOf(q db.Queryer, connID int64, now time.Time) (Health, error) {
	since := now.AddDate(0, 0, -healthDays+1).Format(time.DateOnly)
	raw, err := data.HealthSince(q, connID, since)
	if err != nil {
		return Health{}, err
	}
	if raw.Today, err = data.Fetches(q, connID, now.Format(time.DateOnly)); err != nil {
		return Health{}, err
	}
	return healthFrom(raw), nil
}

// healthAll is every connection's health by id, in one query.
func healthAll(q db.Queryer, now time.Time) (map[int64]Health, error) {
	since := now.AddDate(0, 0, -healthDays+1).Format(time.DateOnly)
	raw, err := data.HealthAll(q, since, now.Format(time.DateOnly))
	if err != nil {
		return nil, err
	}
	out := make(map[int64]Health, len(raw))
	for id, r := range raw {
		out[id] = healthFrom(r)
	}
	return out, nil
}

// healthFrom turns summed fetch outcomes into a Health.
func healthFrom(raw data.ConnHealth) Health {
	h := Health{Fetches: raw.OK + raw.Fail, LastError: raw.LastError, Today: raw.Today}
	if h.Fetches > 0 {
		h.FailPct = raw.Fail * 100 / h.Fetches
		h.AvgMS = int(raw.MsSum / int64(h.Fetches))
	}
	if raw.LastOKAt != "" {
		h.LastOK, _ = db.ParseTime(raw.LastOKAt)
	}
	if raw.LastFailAt != "" {
		h.LastFail, _ = db.ParseTime(raw.LastFailAt)
	}
	return h
}

// SetHygiene stores a token's expiry ("" = unknown) and the daily fetch
// budget (0 = unlimited). Requires MANAGE.
func SetHygiene(d *sql.DB, who *access.Principal, connID int64, expires string, budget int) error {
	if expires != "" {
		if _, err := time.Parse(time.DateOnly, expires); err != nil {
			return ErrBadDate
		}
	}
	defer svcdata.Forget(connID) // the budget applies from the next fetch

	return db.WithTx(d, func(tx *sql.Tx) error {
		conn, err := content.Connection(tx, connID)
		if err != nil {
			return err
		}
		if conn == nil {
			return ErrNotFound
		}
		granted, err := rightOf(tx, who, conn)
		if err != nil {
			return err
		}
		if err := access.Need(granted, enums.RightManage); err != nil {
			return err
		}
		conn.SecretExpires, conn.DailyBudget = expires, max(budget, 0)
		if err := content.UpdateConnection(tx, conn); err != nil {
			return err
		}
		return audit.Log(tx, &who.UserID, "connection.hygiene", conn.Name, "", nil)
	})
}

// SetRefresh stores how often the connection's main query runs, in
// minutes (0 = automatic, see svcdata.Pace). Requires MANAGE.
func SetRefresh(d *sql.DB, who *access.Principal, connID int64, minutes int) error {
	defer svcdata.Forget(connID) // the interval applies from the next fetch

	return db.WithTx(d, func(tx *sql.Tx) error {
		conn, err := content.Connection(tx, connID)
		if err != nil {
			return err
		}
		if conn == nil {
			return ErrNotFound
		}
		granted, err := rightOf(tx, who, conn)
		if err != nil {
			return err
		}
		if err := access.Need(granted, enums.RightManage); err != nil {
			return err
		}
		conn.RefreshMinutes = max(minutes, 0)
		if err := content.UpdateConnection(tx, conn); err != nil {
			return err
		}
		return audit.Log(tx, &who.UserID, "connection.refresh", conn.Name, "", nil)
	})
}

// DayState is one day of a connection's health strip.
type DayState struct {
	Day      string
	OK, Fail int
}

// Strip is one connection's recent days, oldest first, with one entry
// per calendar day (days without fetches have zero counts).
type Strip struct {
	Name, Service string
	FailPct       int
	Fetches       int // in the window
	Days          []DayState
	ID            int64
	LastError     string // of the newest day with a failure
	AvgMs         int    // mean time of the successful fetches, 0 = none
	Tiles         int    // widgets of the space that use it
}

// stripFailPct is the share of failed fetches in percent, rounded up:
// 1 failure in 500 is 1 %, not a clean 0 %.
func stripFailPct(ok, fail int) int {
	total := ok + fail
	if total == 0 {
		return 0
	}
	return (fail*100 + total - 1) / total
}

// Strips returns the day-by-day health of every connection who can see,
// over the last days (the connection health tile).
func Strips(d *sql.DB, who *access.Principal, days int, now time.Time) ([]Strip, error) {
	list, err := Listing(d, who, enums.RightView)
	if err != nil {
		return nil, err
	}
	first := now.AddDate(0, 0, -days+1)
	since := first.Format(time.DateOnly)

	// Days, health and tiles of all connections at once, not per strip.
	var out []Strip
	err = db.WithRead(d, func(tx *sql.Tx) error {
		byConn, err := data.DaysAll(tx, since)
		if err != nil {
			return err
		}
		healths, err := data.HealthAll(tx, since, now.Format(time.DateOnly))
		if err != nil {
			return err
		}
		tiles := map[int64]map[int64]int{} // space → connection → widgets
		for _, c := range list {
			if tiles[c.SpaceID] == nil {
				if tiles[c.SpaceID], err = tilesOf(tx, c.SpaceID); err != nil {
					return err
				}
			}
			out = append(out, stripOf(c, first, days, byConn[c.ID], healths[c.ID], tiles[c.SpaceID][c.ID]))
		}
		return nil
	})
	return out, err
}

// History returns one connection's strip over the last days (its record).
// Requires USE.
func History(d *sql.DB, who *access.Principal, connID int64, days int, now time.Time) (Strip, error) {
	c, err := Get(d, who, connID)
	if err != nil {
		return Strip{}, err
	}
	first := now.AddDate(0, 0, -days+1)
	since := first.Format(time.DateOnly)

	var out Strip
	err = db.WithRead(d, func(tx *sql.Tx) error {
		raw, err := data.DaysSince(tx, c.ID, since)
		if err != nil {
			return err
		}
		h, err := data.HealthSince(tx, c.ID, since)
		if err != nil {
			return err
		}
		tiles, err := tilesOf(tx, c.SpaceID)
		if err != nil {
			return err
		}
		out = stripOf(c, first, days, raw, h, tiles[c.ID])
		return nil
	})
	return out, err
}

// tilesOf counts the widgets of a space by the connection they use.
func tilesOf(q db.Queryer, spaceID int64) (map[int64]int, error) {
	ws, err := content.Widgets(q, []int64{spaceID})
	if err != nil {
		return nil, err
	}
	out := map[int64]int{}
	for _, w := range ws {
		if w.ConnectionID != nil {
			out[*w.ConnectionID]++
		}
	}
	return out, nil
}

// stripOf builds c's strip from its days from first on (raw), their
// sums (h) and the widgets of its space that use it.
func stripOf(c View, first time.Time, days int, raw []data.ConnDay, h data.ConnHealth, tiles int) Strip {
	byDay := map[string]data.ConnDay{}
	ok, fail := 0, 0
	for _, r := range raw {
		byDay[r.Day] = r
		ok, fail = ok+r.OK, fail+r.Fail
	}

	strip := Strip{Name: c.Name, Service: string(c.Service), ID: c.ID, Tiles: tiles, LastError: h.LastError}
	strip.FailPct, strip.Fetches = stripFailPct(ok, fail), ok+fail
	if h.OK > 0 {
		strip.AvgMs = int(h.MsSum / int64(h.OK))
	}
	for i := range days {
		day := first.AddDate(0, 0, i).Format(time.DateOnly)
		r := byDay[day]
		strip.Days = append(strip.Days, DayState{Day: day, OK: r.OK, Fail: r.Fail})
	}
	return strip
}
