package links

import "andon/internal/db"

// Adopt hands connection from's memberships and ids to to, a connection
// of the same service: one member per service, so to is in none of
// from's Verbünde yet.
func Adopt(q db.Queryer, from, to int64) error {
	if _, err := q.Exec("UPDATE link_members SET connection_id = ? WHERE connection_id = ?", to, from); err != nil {
		return err
	}
	_, err := q.Exec("UPDATE link_keys SET connection_id = ? WHERE connection_id = ?", to, from)
	return unique(err, ErrKeyTaken)
}
