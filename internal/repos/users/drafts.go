package users

import (
	"database/sql"
	"errors"

	"andon/internal/db"
)

// Draft returns the cases (JSON) of a user's workbench draft, "" none.
func Draft(q db.Queryer, userID int64, bench, place string) (string, error) {
	var cases string
	err := q.QueryRow("SELECT cases FROM bench_drafts WHERE user_id = ? AND bench = ? AND place = ?",
		userID, bench, place).Scan(&cases)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return cases, err
}

// SetDraft stores the cases (JSON) of a user's workbench draft.
func SetDraft(q db.Queryer, userID int64, bench, place, cases string) error {
	_, err := q.Exec(`INSERT INTO bench_drafts (user_id, bench, place, cases, updated_at) VALUES (?,?,?,?,?)
		ON CONFLICT(user_id, bench, place) DO UPDATE SET cases = excluded.cases, updated_at = excluded.updated_at`,
		userID, bench, place, cases, db.TimeStr(now()))
	return err
}

// DropDraft removes a user's workbench draft.
func DropDraft(q db.Queryer, userID int64, bench, place string) error {
	_, err := q.Exec("DELETE FROM bench_drafts WHERE user_id = ? AND bench = ? AND place = ?", userID, bench, place)
	return err
}
