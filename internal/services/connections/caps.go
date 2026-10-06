package connections

// Capabilities of a connection, for its record: every capability its
// service declares, with what the last background fetch found.

import (
	"context"
	"database/sql"

	"andon/internal/caps"
	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
)

// CapState is how far a connection has a capability.
type CapState string

const (
	CapHave    CapState = "have"
	CapMissing CapState = "missing"
	CapUnknown CapState = "unknown" // not fetched yet
)

// CapRow is one declared capability of a connection.
type CapRow struct {
	Cap   caps.Cap
	State CapState
	Need  caps.Need // what is missing, for CapMissing
}

// Capabilities lists what the connection can do, from its stored
// dataset; nil for services that declare nothing. Requires USE.
func Capabilities(ctx context.Context, d *sql.DB, who *access.Principal, connID int64) ([]CapRow, error) {
	conn, err := usable(d, who, connID)
	if err != nil {
		return nil, err
	}
	declared := caps.Declared(caps.HolderOf(enums.ServiceType(conn.Service)))
	if len(declared) == 0 {
		return nil, nil
	}

	set, known := storedSet(ctx, d, who, conn)
	out := make([]CapRow, 0, len(declared))
	for _, c := range declared {
		out = append(out, rowOf(c, set, known))
	}
	return out, nil
}

// CapSetOf is what the connection could do at its last background fetch;
// known is false before the first one. Requires USE.
func CapSetOf(ctx context.Context, d *sql.DB, who *access.Principal, connID int64) (set caps.Set, known bool, err error) {
	conn, err := usable(d, who, connID)
	if err != nil {
		return caps.Set{}, false, err
	}
	set, known = storedSet(ctx, d, who, conn)
	return set, known, nil
}

// rowOf finds c in set: had, missing (with the need) or unknown.
func rowOf(c caps.Cap, set caps.Set, known bool) CapRow {
	if !known {
		return CapRow{Cap: c, State: CapUnknown}
	}
	if g, missing := set.Gap(c); missing {
		return CapRow{Cap: c, State: CapMissing, Need: g.Need}
	}
	return CapRow{Cap: c, State: CapHave}
}

// storedSet is the capability set of the last background fetch.
func storedSet(ctx context.Context, d *sql.DB, who *access.Principal, conn *model.Connection) (caps.Set, bool) {
	result, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceType(conn.Service)), nil, conn, model.UserHolder(who.UserID), svcdata.Stored)
	if err != nil || !result.Ok() {
		return caps.Set{}, false
	}
	r, ok := result.Data.(caps.Reporter)
	if !ok {
		return caps.Set{}, false
	}
	return r.CapSet(), true
}

// usable loads a connection the caller may USE.
func usable(d *sql.DB, who *access.Principal, connID int64) (*model.Connection, error) {
	var conn *model.Connection
	err := db.WithRead(d, func(tx *sql.Tx) error {
		c, err := content.Connection(tx, connID)
		if err != nil {
			return err
		}
		if c == nil {
			return ErrNotFound
		}
		granted, err := rightOf(tx, who, c)
		if err != nil {
			return err
		}
		if err := access.Need(granted, enums.RightUse); err != nil {
			return err
		}
		conn = c
		return nil
	})
	return conn, err
}
