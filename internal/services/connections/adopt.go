package connections

// Adopting: a second connection of the same service, set up for the new
// server, takes over what hangs on the old one, then the old one goes.
//
//	old ──tiles, Verbund ids, hints (marks, notes), history, shares──► new
//	old ──delete (its logins, grants, cache)
//
// Ids in the Verbund stay as they were: if the new server is another
// install with other ids, the Verbund lists those as orphans to check.

import (
	"database/sql"
	"errors"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/repos/data"
	"andon/internal/repos/links"
	"andon/internal/repos/misc"
	"andon/internal/services/access"
	"andon/internal/services/audit"
	"andon/internal/services/svcdata"
)

var (
	// ErrAdoptSelf: a connection cannot take over from itself.
	ErrAdoptSelf = errors.New("connection.adopt_self")
	// ErrAdoptService: only a connection of the same service.
	ErrAdoptService = errors.New("connection.adopt_service")
	// ErrAdoptSpace: only a connection of the same space (tiles, hints
	// and history belong to it).
	ErrAdoptSpace = errors.New("connection.adopt_space")
)

// Adopt makes connection to take over everything bound to connection
// from, then deletes from. Requires MANAGE on both.
func Adopt(d *sql.DB, who *access.Principal, to, from int64) error {
	defer svcdata.Forget(to)
	defer svcdata.Forget(from)

	if to == from {
		return ErrAdoptSelf
	}
	return db.WithTx(d, func(tx *sql.Tx) error {
		target, err := managed(tx, who, to)
		if err != nil {
			return err
		}
		source, err := managed(tx, who, from)
		if err != nil {
			return err
		}
		if source.Service != target.Service {
			return ErrAdoptService
		}
		if source.SpaceID != target.SpaceID {
			return ErrAdoptSpace
		}

		if err := content.AdoptWidgets(tx, target.SpaceID, from, to, source.Key, target.Key); err != nil {
			return err
		}
		if err := links.Adopt(tx, from, to); err != nil {
			return err
		}
		if err := data.AdoptHints(tx, from, to); err != nil {
			return err
		}
		if err := data.AdoptHistory(tx, from, to); err != nil {
			return err
		}
		if err := misc.AdoptShares(tx, from, to); err != nil {
			return err
		}

		if err := audit.Log(tx, &who.UserID, "connection.adopted", target.Name, "", map[string]any{"from": source.Name}); err != nil {
			return err
		}
		if err := misc.DropShares(tx, enums.ResourceConnection, from); err != nil {
			return err
		}
		return content.RemoveConnection(tx, from)
	})
}

// managed reads a connection the caller may manage.
func managed(q db.Queryer, who *access.Principal, connID int64) (*model.Connection, error) {
	conn, err := content.Connection(q, connID)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, ErrNotFound
	}
	granted, err := rightOf(q, who, conn)
	if err != nil {
		return nil, err
	}
	if err := access.Need(granted, enums.RightManage); err != nil {
		return nil, err
	}
	return conn, nil
}

// AdoptCandidates lists the connections to may take over from: the
// others of its service in its space the caller manages.
func AdoptCandidates(d *sql.DB, who *access.Principal, to int64) ([]View, error) {
	target, err := Get(d, who, to)
	if err != nil {
		return nil, err
	}
	all, err := Listing(d, who, enums.RightManage)
	if err != nil {
		return nil, err
	}
	var out []View
	for _, c := range all {
		if c.ID != to && c.Service == target.Service && c.SpaceID == target.SpaceID {
			out = append(out, c)
		}
	}
	return out, nil
}
