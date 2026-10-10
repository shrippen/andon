package content

import "andon/internal/db"

// AdoptWidgets points connection from's tiles at to, and the link tiles
// of the space whose info line names from by key (info.connection).
func AdoptWidgets(q db.Queryer, spaceID, from, to int64, fromKey, toKey string) error {
	if _, err := q.Exec("UPDATE widgets SET connection_id = ? WHERE connection_id = ?", to, from); err != nil {
		return err
	}
	_, err := q.Exec(`UPDATE widgets SET config = json_set(config, '$.info.connection', ?)
		WHERE space_id = ? AND json_extract(config, '$.info.connection') = ?`, toKey, spaceID, fromKey)
	return err
}
