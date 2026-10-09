package data

import (
	"strconv"

	"andon/internal/db"
)

// AdoptHints hands connection from's hints to to. Their fingerprints
// carry the connection ("7:kimai.over" → "9:kimai.over"); where to has
// the same hint already, from's wins, with its marks, notes and events.
func AdoptHints(q db.Queryer, from, to int64) error {
	fromPrefix, toPrefix := strconv.FormatInt(from, 10)+":", strconv.FormatInt(to, 10)+":"
	if _, err := q.Exec(`DELETE FROM hints WHERE connection_id = ?
		AND (space_id, IFNULL(user_id, 0), substr(fingerprint, ?)) IN
			(SELECT space_id, IFNULL(user_id, 0), substr(fingerprint, ?) FROM hints WHERE connection_id = ? AND fingerprint LIKE ?)`,
		to, len(toPrefix)+1, len(fromPrefix)+1, from, fromPrefix+"%"); err != nil {
		return err
	}
	if _, err := q.Exec(`UPDATE hints SET fingerprint = ? || substr(fingerprint, ?) WHERE connection_id = ? AND fingerprint LIKE ?`,
		toPrefix, len(fromPrefix)+1, from, fromPrefix+"%"); err != nil {
		return err
	}
	_, err := q.Exec("UPDATE hints SET connection_id = ? WHERE connection_id = ?", to, from)
	return err
}

// AdoptHistory hands connection from's fetch history, webhook events
// and mail reads to to; days both have add up.
func AdoptHistory(q db.Queryer, from, to int64) error {
	if _, err := q.Exec(`INSERT INTO conn_stats (connection_id, day, ok, fail, ms_sum, last_ok_at, last_fail_at, last_error)
		SELECT ?, day, ok, fail, ms_sum, last_ok_at, last_fail_at, last_error FROM conn_stats WHERE connection_id = ? AND true
		ON CONFLICT (connection_id, day) DO UPDATE SET ok = ok + excluded.ok, fail = fail + excluded.fail,
			ms_sum = ms_sum + excluded.ms_sum, last_ok_at = max(last_ok_at, excluded.last_ok_at),
			last_fail_at = max(last_fail_at, excluded.last_fail_at),
			last_error = CASE WHEN excluded.last_fail_at > last_fail_at THEN excluded.last_error ELSE last_error END`,
		to, from); err != nil {
		return err
	}
	if _, err := q.Exec("DELETE FROM conn_stats WHERE connection_id = ?", from); err != nil {
		return err
	}
	if _, err := q.Exec("UPDATE hook_events SET connection_id = ? WHERE connection_id = ?", to, from); err != nil {
		return err
	}
	_, err := q.Exec("UPDATE OR IGNORE mail_reads SET connection_id = ? WHERE connection_id = ?", to, from)
	return err
}
