// Package verbund keeps Verbünde: connections of different services that
// work together (Dawarich + Kimai + Invoice Ninja …), and finds a
// connection's partner in another service:
//
//	asker (tile, connection, space) ──Partner(S)──► chosen Verbund's S member
//	                                              ► the S member of the one Verbund the connection is in
//	                                              ► the one S connection of the space (implicit)
//	                                              ► the one S connection the user reaches
//	                                              ► none, or ambiguous: the user decides
//
// A Verbund holds one connection per service; changing it needs EDIT on
// every member, seeing it VIEW on every member.
package verbund

import (
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	linkrepo "andon/internal/repos/links"
	"andon/internal/services/access"
	"andon/internal/services/audit"
)

var (
	// ErrNotFound: no such Verbund, or not visible to the caller.
	ErrNotFound = errors.New("verbund.not_found")
	// ErrTooFew: a Verbund needs two connections.
	ErrTooFew = errors.New("verbund.too_few")
	// ErrServiceTwice: one connection per service.
	ErrServiceTwice = errors.New("verbund.service_twice")
	// ErrNoName: a Verbund needs a name.
	ErrNoName = errors.New("verbund.no_name")
)

// minMembers is the size of the smallest Verbund.
const minMembers = 2

// Member is a connection of a Verbund, as the caller sees it.
type Member struct {
	ConnID  int64
	Name    string
	Service enums.ServiceType
	SpaceID int64
}

// View is a Verbund with its members.
type View struct {
	ID      int64
	Name    string
	Members []Member
	CanEdit bool // the caller has EDIT on every member
}

// Has reports whether the Verbund has a member of service.
func (v View) Has(service enums.ServiceType) (Member, bool) {
	for _, m := range v.Members {
		if m.Service == service {
			return m, true
		}
	}
	return Member{}, false
}

// HasService reports whether the Verbund has a member of service.
func (v View) HasService(service enums.ServiceType) bool {
	_, ok := v.Has(service)
	return ok
}

// rightOn is the caller's right on a connection.
func rightOn(q db.Queryer, who *access.Principal, conn *model.Connection) (enums.Right, error) {
	space, err := access.SpaceOf(q, who, conn.SpaceID)
	if err != nil {
		return enums.RightNone, err
	}
	return access.Right(who, enums.ResourceConnection, conn.ID, space, nil), nil
}

// viewOf turns a stored Verbund into the caller's view; ok is false when
// the caller may not see every member.
func viewOf(q db.Queryer, who *access.Principal, l linkrepo.Link) (View, bool, error) {
	v := View{ID: l.ID, Name: l.Name, CanEdit: true}
	for _, m := range l.Members {
		conn, err := content.Connection(q, m.ConnID)
		if err != nil {
			return View{}, false, err
		}
		if conn == nil {
			return View{}, false, nil
		}
		right, err := rightOn(q, who, conn)
		if err != nil {
			return View{}, false, err
		}
		if right < enums.RightView {
			return View{}, false, nil
		}
		v.CanEdit = v.CanEdit && right >= enums.RightEdit
		v.Members = append(v.Members, Member{ConnID: conn.ID, Name: conn.Name, Service: enums.ServiceType(conn.Service), SpaceID: conn.SpaceID})
	}
	return v, true, nil
}

// visible lists the Verbünde the caller sees, filtered by keep.
func visible(q db.Queryer, who *access.Principal, keep func(linkrepo.Link) bool) ([]View, error) {
	all, err := linkrepo.All(q)
	if err != nil {
		return nil, err
	}
	var out []View
	for _, l := range all {
		if !keep(l) {
			continue
		}
		v, ok, err := viewOf(q, who, l)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, v)
		}
	}
	return out, nil
}

// List returns the Verbünde with a member in the space.
func List(d *sql.DB, who *access.Principal, spaceID int64) ([]View, error) {
	var out []View
	err := db.WithRead(d, func(tx *sql.Tx) error {
		inSpace, err := content.Connections(tx, []int64{spaceID})
		if err != nil {
			return err
		}
		ids := map[int64]bool{}
		for _, c := range inSpace {
			ids[c.ID] = true
		}
		out, err = visible(tx, who, func(l linkrepo.Link) bool {
			return slices.ContainsFunc(l.Members, func(m linkrepo.Member) bool { return ids[m.ConnID] })
		})
		return err
	})
	return out, err
}

// Of returns the Verbünde the connection is in.
func Of(d *sql.DB, who *access.Principal, connID int64) ([]View, error) {
	var out []View
	err := db.WithRead(d, func(tx *sql.Tx) error {
		var err error
		out, err = visible(tx, who, func(l linkrepo.Link) bool { return hasConn(l, connID) })
		return err
	})
	return out, err
}

// Get returns one Verbund the caller sees.
func Get(d *sql.DB, who *access.Principal, id int64) (View, error) {
	var out View
	err := db.WithRead(d, func(tx *sql.Tx) error {
		l, err := linkrepo.Get(tx, id)
		if err != nil {
			return err
		}
		if l == nil {
			return ErrNotFound
		}
		v, ok, err := viewOf(tx, who, *l)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
		out = v
		return nil
	})
	return out, err
}

func hasConn(l linkrepo.Link, connID int64) bool {
	return slices.ContainsFunc(l.Members, func(m linkrepo.Member) bool { return m.ConnID == connID })
}

// editable loads connections the caller may EDIT and checks one per
// service.
func editable(q db.Queryer, who *access.Principal, connIDs []int64) ([]*model.Connection, error) {
	seen := map[string]bool{}
	var out []*model.Connection
	for _, id := range connIDs {
		conn, err := content.Connection(q, id)
		if err != nil {
			return nil, err
		}
		if conn == nil {
			return nil, ErrNotFound
		}
		right, err := rightOn(q, who, conn)
		if err != nil {
			return nil, err
		}
		if err := access.Need(right, enums.RightEdit); err != nil {
			return nil, err
		}
		if seen[conn.Service] {
			return nil, ErrServiceTwice
		}
		seen[conn.Service] = true
		out = append(out, conn)
	}
	return out, nil
}

// Create makes a Verbund of two or more connections of different
// services. Requires EDIT on each.
func Create(d *sql.DB, who *access.Principal, name string, connIDs []int64, ip string) (int64, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, ErrNoName
	}
	connIDs = unique(connIDs)
	if len(connIDs) < minMembers {
		return 0, ErrTooFew
	}

	var id int64
	err := db.WithTx(d, func(tx *sql.Tx) error {
		conns, err := editable(tx, who, connIDs)
		if err != nil {
			return err
		}
		id, err = linkrepo.Add(tx, name, &who.UserID, time.Now().UTC())
		if err != nil {
			return err
		}
		for _, c := range conns {
			if err := linkrepo.AddMember(tx, id, c.ID, c.Service); err != nil {
				return err
			}
		}
		return audit.Log(tx, &who.UserID, "verbund.created", name, ip, map[string]any{"connections": connIDs})
	})
	return id, err
}

// change runs fn on a Verbund the caller may EDIT completely.
func change(d *sql.DB, who *access.Principal, id int64, fn func(tx *sql.Tx, l *linkrepo.Link) error) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		l, err := linkrepo.Get(tx, id)
		if err != nil {
			return err
		}
		if l == nil {
			return ErrNotFound
		}
		v, ok, err := viewOf(tx, who, *l)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNotFound
		}
		if !v.CanEdit {
			return access.ErrDenied
		}
		return fn(tx, l)
	})
}

// Rename changes the name. Requires EDIT on every member.
func Rename(d *sql.DB, who *access.Principal, id int64, name, ip string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrNoName
	}
	return change(d, who, id, func(tx *sql.Tx, l *linkrepo.Link) error {
		if err := linkrepo.Rename(tx, id, name); err != nil {
			return err
		}
		return audit.Log(tx, &who.UserID, "verbund.renamed", name, ip, nil)
	})
}

// AddMember puts a connection into the Verbund. Requires EDIT on every
// member and on the connection.
func AddMember(d *sql.DB, who *access.Principal, id, connID int64, ip string) error {
	return change(d, who, id, func(tx *sql.Tx, l *linkrepo.Link) error {
		conns, err := editable(tx, who, []int64{connID})
		if err != nil {
			return err
		}
		if err := linkrepo.AddMember(tx, id, connID, conns[0].Service); err != nil {
			if errors.Is(err, linkrepo.ErrServiceTaken) {
				return ErrServiceTwice
			}
			return err
		}
		return audit.Log(tx, &who.UserID, "verbund.member_added", l.Name, ip, map[string]any{"connection": connID})
	})
}

// RemoveMember takes a connection out; with one member left the
// Verbund goes. Requires EDIT on every member.
func RemoveMember(d *sql.DB, who *access.Principal, id, connID int64, ip string) error {
	return change(d, who, id, func(tx *sql.Tx, l *linkrepo.Link) error {
		if !hasConn(*l, connID) {
			return ErrNotFound
		}
		if err := linkrepo.RemoveMember(tx, id, connID); err != nil {
			return err
		}
		return audit.Log(tx, &who.UserID, "verbund.member_removed", l.Name, ip, map[string]any{"connection": connID})
	})
}

// Delete removes the Verbund and its entries; the connections stay.
// Requires EDIT on every member.
func Delete(d *sql.DB, who *access.Principal, id int64, ip string) error {
	return change(d, who, id, func(tx *sql.Tx, l *linkrepo.Link) error {
		if err := linkrepo.Delete(tx, id); err != nil {
			return err
		}
		return audit.Log(tx, &who.UserID, "verbund.deleted", l.Name, ip, nil)
	})
}

// unique drops repeated ids, keeping the order.
func unique(ids []int64) []int64 {
	var out []int64
	for _, id := range ids {
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// Visible lists every Verbund the caller sees.
func Visible(d *sql.DB, who *access.Principal) ([]View, error) {
	var out []View
	err := db.WithRead(d, func(tx *sql.Tx) error {
		var err error
		out, err = visible(tx, who, func(linkrepo.Link) bool { return true })
		return err
	})
	return out, err
}

// Ambiguous lists the services of the space with several connections
// that no Verbund tells apart (the analysis' hint, for the page).
func Ambiguous(d *sql.DB, who *access.Principal, spaceID int64) ([]string, error) {
	if _, ok := who.Spaces[spaceID]; !ok {
		return nil, access.ErrDenied
	}
	var out []string
	err := db.WithRead(d, func(tx *sql.Tx) error {
		conns, err := content.Connections(tx, []int64{spaceID})
		if err != nil {
			return err
		}
		stored, err := linkrepo.All(tx)
		if err != nil {
			return err
		}
		_, out = Groups(conns, stored)
		return nil
	})
	return out, err
}
