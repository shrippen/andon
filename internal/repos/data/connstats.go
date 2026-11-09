package data

import (
	"time"

	"andon/internal/db"
)

// ConnHealth sums a connection's fetches over some days.
type ConnHealth struct {
	OK, Fail   int
	MsSum      int64
	LastOKAt   string // db time, "" = never
	LastFailAt string // db time, "" = never
	LastError  string // of the newest day with a failure
	Today      int    // fetches of the last day (HealthAll)
}

// RecordFetch adds one fetch outcome to today's row.
func RecordFetch(q db.Queryer, connID int64, at time.Time, ms int64, errMsg string) error {
	ok, fail, lastOK, lastFail := 1, 0, db.TimeStr(at), ""
	if errMsg != "" {
		ok, fail, lastOK, lastFail = 0, 1, "", db.TimeStr(at)
	}
	_, err := q.Exec(`INSERT INTO conn_stats (connection_id, day, ok, fail, ms_sum, last_ok_at, last_fail_at, last_error) VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT (connection_id, day) DO UPDATE SET ok = ok + excluded.ok, fail = fail + excluded.fail,
			ms_sum = ms_sum + excluded.ms_sum,
			last_ok_at = CASE WHEN excluded.last_ok_at = '' THEN last_ok_at ELSE excluded.last_ok_at END,
			last_fail_at = CASE WHEN excluded.last_fail_at = '' THEN last_fail_at ELSE excluded.last_fail_at END,
			last_error = CASE WHEN excluded.last_error = '' THEN last_error ELSE excluded.last_error END`,
		connID, at.Format(time.DateOnly), ok, fail, ms, lastOK, lastFail, errMsg)
	return err
}

// Fetches counts a connection's fetches of one day ("2026-09-25").
func Fetches(q db.Queryer, connID int64, day string) (int, error) {
	var n int
	err := q.QueryRow("SELECT COALESCE(SUM(ok + fail), 0) FROM conn_stats WHERE connection_id = ? AND day = ?", connID, day).Scan(&n)
	return n, err
}

// HealthSince sums a connection's rows from a day on.
func HealthSince(q db.Queryer, connID int64, since string) (ConnHealth, error) {
	var h ConnHealth
	err := q.QueryRow(`SELECT COALESCE(SUM(ok), 0), COALESCE(SUM(fail), 0), COALESCE(SUM(ms_sum), 0),
			COALESCE(MAX(last_ok_at), ''), COALESCE(MAX(last_fail_at), ''),
			COALESCE((SELECT last_error FROM conn_stats WHERE connection_id = ?1 AND day >= ?2 AND last_error != '' ORDER BY day DESC LIMIT 1), '')
		FROM conn_stats WHERE connection_id = ?1 AND day >= ?2`, connID, since).
		Scan(&h.OK, &h.Fail, &h.MsSum, &h.LastOKAt, &h.LastFailAt, &h.LastError)
	return h, err
}

// HealthAll sums every connection's rows from since on, plus its
// fetches of today, in one query (a listing needs them all).
func HealthAll(q db.Queryer, since, today string) (map[int64]ConnHealth, error) {
	rows, err := q.Query(`SELECT connection_id, SUM(ok), SUM(fail), SUM(ms_sum), MAX(last_ok_at), MAX(last_fail_at),
			SUM(CASE WHEN day = ?2 THEN ok + fail ELSE 0 END),
			COALESCE((SELECT e.last_error FROM conn_stats e WHERE e.connection_id = s.connection_id AND e.day >= ?1 AND e.last_error != '' ORDER BY e.day DESC LIMIT 1), '')
		FROM conn_stats s WHERE day >= ?1 GROUP BY connection_id`, since, today)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64]ConnHealth{}
	for rows.Next() {
		var id int64
		var h ConnHealth
		if err := rows.Scan(&id, &h.OK, &h.Fail, &h.MsSum, &h.LastOKAt, &h.LastFailAt, &h.Today, &h.LastError); err != nil {
			return nil, err
		}
		out[id] = h
	}
	return out, rows.Err()
}

// PruneConnStats drops rows before a day.
func PruneConnStats(q db.Queryer, before string) error {
	_, err := q.Exec("DELETE FROM conn_stats WHERE day < ?", before)
	return err
}

// ConnDay is one day of a connection's fetch outcomes.
type ConnDay struct {
	Day      string
	OK, Fail int
}

// DaysAll lists every connection's days from since on by id, oldest
// first; days without fetches are missing.
func DaysAll(q db.Queryer, since string) (map[int64][]ConnDay, error) {
	rows, err := q.Query("SELECT connection_id, day, ok, fail FROM conn_stats WHERE day >= ? ORDER BY connection_id, day", since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64][]ConnDay{}
	for rows.Next() {
		var id int64
		var c ConnDay
		if err := rows.Scan(&id, &c.Day, &c.OK, &c.Fail); err != nil {
			return nil, err
		}
		out[id] = append(out[id], c)
	}
	return out, rows.Err()
}

// DaysSince lists a connection's days from since on, oldest first; days
// without fetches are missing.
func DaysSince(q db.Queryer, connID int64, since string) ([]ConnDay, error) {
	rows, err := q.Query("SELECT day, ok, fail FROM conn_stats WHERE connection_id = ? AND day >= ? ORDER BY day", connID, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ConnDay
	for rows.Next() {
		var d ConnDay
		if err := rows.Scan(&d.Day, &d.OK, &d.Fail); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
