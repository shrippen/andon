package misc

import (
	"database/sql"
	"errors"
	"strings"
	"time"

	"andon/internal/db"
	"andon/internal/model"
)

// LearnedModel is one stored model: its examples as JSON.
type LearnedModel struct {
	Purpose   string
	ConnID    int64
	Holder    model.Holder
	Model     string
	TrainedAt time.Time
}

// Learned reads the model of purpose for a connection and holder; ok
// false if none.
func Learned(q db.Queryer, purpose string, connID int64, h model.Holder) (LearnedModel, bool, error) {
	m := LearnedModel{Purpose: purpose, ConnID: connID, Holder: h}
	var at string
	err := q.QueryRow("SELECT model, trained_at FROM learned_models WHERE purpose = ? AND conn_id = ? AND holder = ?", purpose, connID, h).Scan(&m.Model, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return m, false, nil
	}
	if err != nil {
		return m, false, err
	}
	m.TrainedAt, err = db.ParseTime(at)
	return m, true, err
}

// AllLearned lists every stored model.
func AllLearned(q db.Queryer) ([]LearnedModel, error) {
	rows, err := q.Query("SELECT purpose, conn_id, holder, model, trained_at FROM learned_models ORDER BY conn_id, purpose, holder")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LearnedModel
	for rows.Next() {
		var m LearnedModel
		var at string
		if err := rows.Scan(&m.Purpose, &m.ConnID, &m.Holder, &m.Model, &at); err != nil {
			return nil, err
		}
		if m.TrainedAt, err = db.ParseTime(at); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetLearned stores a model.
func SetLearned(q db.Queryer, m LearnedModel) error {
	_, err := q.Exec(`INSERT INTO learned_models (purpose, conn_id, holder, user_id, team_id, model, trained_at) VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(purpose, conn_id, holder) DO UPDATE SET model = excluded.model, trained_at = excluded.trained_at`,
		m.Purpose, m.ConnID, m.Holder, idOrNil(m.Holder.User()), idOrNil(m.Holder.Team()), m.Model, db.TimeStr(m.TrainedAt))
	return err
}

// TouchLearned marks a model trained at now, unchanged.
func TouchLearned(q db.Queryer, purpose string, connID int64, h model.Holder, now time.Time) error {
	_, err := q.Exec("UPDATE learned_models SET trained_at = ? WHERE purpose = ? AND conn_id = ? AND holder = ?", db.TimeStr(now), purpose, connID, h)
	return err
}

// DeleteLearned forgets a model.
func DeleteLearned(q db.Queryer, purpose string, connID int64, h model.Holder) error {
	_, err := q.Exec("DELETE FROM learned_models WHERE purpose = ? AND conn_id = ? AND holder = ?", purpose, connID, h)
	return err
}

// idOrNil is NULL for 0: no user or team.
func idOrNil(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// PruneLearned forgets the models of a connection whose holder is not
// among keep: a login taken back.
func PruneLearned(q db.Queryer, connID int64, keep []model.Holder) error {
	args := []any{connID}
	marks := make([]string, len(keep))
	for i, h := range keep {
		marks[i], args = "?", append(args, h)
	}
	query := "DELETE FROM learned_models WHERE conn_id = ?"
	if len(keep) > 0 {
		query += " AND holder NOT IN (" + strings.Join(marks, ",") + ")"
	}
	_, err := q.Exec(query, args...)
	return err
}
