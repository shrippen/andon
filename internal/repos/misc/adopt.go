package misc

import (
	"andon/internal/db"
	"andon/internal/enums"
)

// AdoptShares hands connection from's shares to to; a grantee to is
// shared with already keeps that share.
func AdoptShares(q db.Queryer, from, to int64) error {
	_, err := q.Exec("UPDATE OR IGNORE shares SET resource_id = ? WHERE resource_kind = ? AND resource_id = ?",
		to, enums.ResourceConnection, from)
	return err
}
