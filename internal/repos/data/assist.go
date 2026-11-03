package data

import (
	"database/sql"
	"errors"
	"maps"
	"time"

	"andon/internal/db"
	"andon/internal/model"
)

// Advice returns a stored answer for a hint if its digest still matches.
func Advice(q db.Queryer, hintID int64, locale, digest string) (string, bool, error) {
	var text string
	err := q.QueryRow("SELECT text FROM hint_advice WHERE hint_id = ? AND locale = ? AND digest = ?", hintID, locale, digest).Scan(&text)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return text, err == nil, err
}

// SaveAdvice stores or replaces a hint's answer.
func SaveAdvice(q db.Queryer, hintID int64, locale, digest, text string) error {
	_, err := q.Exec(`INSERT INTO hint_advice (hint_id, locale, digest, text, created_at) VALUES (?,?,?,?,?)
		ON CONFLICT (hint_id, locale) DO UPDATE SET digest = excluded.digest, text = excluded.text, created_at = excluded.created_at`,
		hintID, locale, digest, text, db.TimeStr(time.Now().UTC()))
	return err
}

// MailRead is what is stored on one mail of a holder's mailbox: read
// invoice fields and marks. Mail is the mail's key; "" for a row from
// before keys, which matches by UID alone.
type MailRead struct {
	Holder model.Holder
	UID    uint32
	Mail   string
	Fields map[string]any
}

// MailReads returns the stored reads of a mailbox, every holder's.
func MailReads(q db.Queryer, connID int64) ([]MailRead, error) {
	rows, err := q.Query("SELECT holder, uid, mail, data FROM mail_reads WHERE connection_id = ?", connID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MailRead
	for rows.Next() {
		var r MailRead
		var raw string
		if err := rows.Scan(&r.Holder, &r.UID, &r.Mail, &raw); err != nil {
			return nil, err
		}
		r.Fields = map[string]any{}
		if err := db.FromJSON(raw, &r.Fields); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReadOf is a copy of the fields of holder h's mail (uid, key mail):
// its own row, else one from before keys, matched by UID alone as then;
// nil if none.
func ReadOf(rows []MailRead, h model.Holder, uid uint32, mail string) map[string]any {
	var old map[string]any
	for _, r := range rows {
		if r.Holder == h && r.Mail == mail {
			return maps.Clone(r.Fields)
		}
		if r.Mail == "" && r.UID == uid {
			old = r.Fields
		}
	}
	return maps.Clone(old)
}

// SaveMailRead stores or replaces the fields of one holder's mail.
func SaveMailRead(q db.Queryer, connID int64, r MailRead) error {
	raw, err := db.ToJSON(r.Fields)
	if err != nil {
		return err
	}
	_, err = q.Exec(`INSERT INTO mail_reads (connection_id, holder, user_id, team_id, uid, mail, data, created_at) VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT (connection_id, holder, uid, mail) DO UPDATE SET data = excluded.data, created_at = excluded.created_at`,
		connID, r.Holder, idOrNull(r.Holder.User()), idOrNull(r.Holder.Team()), r.UID, r.Mail, raw, db.TimeStr(time.Now().UTC()))
	return err
}

// idOrNull is a holder's user or team id, NULL for none.
func idOrNull(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}
