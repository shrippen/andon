package misc

import (
	"database/sql"
	"errors"
	"time"

	"andon/internal/db"
)

// LearnedModel is one stored model: its examples as JSON.
type LearnedModel struct {
	Purpose   string
	ConnID    int64
	Model     string
	TrainedAt time.Time
}

// Learned reads the model of purpose for a connection; ok false if none.
func Learned(q db.Queryer, purpose string, connID int64) (LearnedModel, bool, error) {
	m := LearnedModel{Purpose: purpose, ConnID: connID}
	var at string
	err := q.QueryRow("SELECT model, trained_at FROM learned_models WHERE purpose = ? AND conn_id = ?", purpose, connID).Scan(&m.Model, &at)
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
	rows, err := q.Query("SELECT purpose, conn_id, model, trained_at FROM learned_models ORDER BY conn_id, purpose")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []LearnedModel
	for rows.Next() {
		var m LearnedModel
		var at string
		if err := rows.Scan(&m.Purpose, &m.ConnID, &m.Model, &at); err != nil {
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
	_, err := q.Exec(`INSERT INTO learned_models (purpose, conn_id, model, trained_at) VALUES (?,?,?,?)
		ON CONFLICT(purpose, conn_id) DO UPDATE SET model = excluded.model, trained_at = excluded.trained_at`,
		m.Purpose, m.ConnID, m.Model, db.TimeStr(m.TrainedAt))
	return err
}

// DeleteLearned forgets a model.
func DeleteLearned(q db.Queryer, purpose string, connID int64) error {
	_, err := q.Exec("DELETE FROM learned_models WHERE purpose = ? AND conn_id = ?", purpose, connID)
	return err
}
