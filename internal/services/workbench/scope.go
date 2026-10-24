package workbench

// A bench works inside one Verbund: the connections of its services that
// work together. Several Verbünde may hold the bench's services, e.g. a
// studio and a side business, each with its own Invoice Ninja; the user
// picks one and the bench remembers it.
//
//	Verbund "Studio": Ninja A, Sure 1, Paperless X ─┐
//	Verbund "Verein": Ninja B, Paperless Y         ─┼─► Scopes ─► pick ─► Scope.Conn(service)
//	Ninja C in no Verbund (partners: verbund.Partner)┘

import (
	"database/sql"
	"slices"
	"strconv"

	"andon/internal/caps"
	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/verbund"
)

// Need names a bench's services: the first of Required anchors a scope
// (one scope per Verbund it is in), the rest must be found; Optional
// ones are used when the Verbund has them. Anchor, when set, anchors on
// any member of a category instead (Snipe-IT or Kept); a Verbund with
// two members is offered once, by its preferred one (caps.Members).
type Need struct {
	Required, Optional []enums.ServiceType
	Anchor             caps.Category
}

// anchors reports whether service anchors a scope of need.
func (n Need) anchors(service enums.ServiceType) bool {
	if n.Anchor != "" {
		c, ok := caps.CategoryOf(service)
		return ok && c == n.Anchor
	}
	return len(n.Required) > 0 && service == n.Required[0]
}

// Scope is one Verbund a bench can work in.
type Scope struct {
	Key        string // "v12" a stored Verbund, "c3" a connection in none
	Name       string // the Verbund's, else the anchor connection's
	Link       int64  // the Verbund, 0 = none
	NeedsToken bool   // a member needs the user's own token, not stored yet
	members    map[enums.ServiceType]*model.Connection
}

// Conn is the scope's connection of service, nil if it has none.
func (s Scope) Conn(service enums.ServiceType) *model.Connection { return s.members[service] }

// Of is the scope's connection of a category, its preferred member; nil
// if it has none.
func (s Scope) Of(c caps.Category) *model.Connection {
	for _, m := range caps.Members(c) {
		if conn := s.members[m]; conn != nil {
			return conn
		}
	}
	return nil
}

// Scope key prefixes.
const (
	keyVerbund = "v"
	keyConn    = "c"
)

// Scopes lists the Verbünde the user can work in with the bench's
// services, in the order of the anchor connections.
func Scopes(d *sql.DB, who *access.Principal, need Need) ([]Scope, error) {
	if len(need.Required) == 0 && need.Anchor == "" {
		return nil, nil
	}
	views, err := connections.Listing(d, who, enums.RightUse)
	if err != nil {
		return nil, err
	}
	usable := map[int64]connections.View{}
	for _, v := range views {
		usable[v.ID] = v
	}

	var out []Scope
	for _, v := range views {
		if !need.anchors(v.Service) {
			continue
		}
		found, err := scopesOf(d, who, v, usable, need)
		if err != nil {
			return nil, err
		}
		out = append(out, found...)
	}
	return out, nil
}

// scopesOf is one anchor connection's scopes: one per Verbund it is in,
// else the one its partners make.
func scopesOf(d *sql.DB, who *access.Principal, anchor connections.View, usable map[int64]connections.View, need Need) ([]Scope, error) {
	links, err := verbund.Of(d, who, anchor.ID)
	if err != nil {
		return nil, err
	}
	if len(links) == 0 {
		links = []verbund.View{{Name: anchor.Name}}
	}

	var out []Scope
	err = db.WithRead(d, func(tx *sql.Tx) error {
		for _, l := range links {
			s, ok, err := scopeIn(tx, who, anchor, l, usable, need)
			if err != nil {
				return err
			}
			if ok {
				out = append(out, s)
			}
		}
		return nil
	})
	return out, err
}

// scopeIn is the anchor's scope in Verbund l (ID 0: none); ok is false
// when a required service is missing.
func scopeIn(tx *sql.Tx, who *access.Principal, anchor connections.View, l verbund.View, usable map[int64]connections.View, need Need) (Scope, bool, error) {
	s := Scope{Name: l.Name, Link: l.ID, Key: keyVerbund + strconv.FormatInt(l.ID, 10), members: map[enums.ServiceType]*model.Connection{}}
	if l.ID == 0 {
		s.Key = keyConn + strconv.FormatInt(anchor.ID, 10)
	}
	if need.Anchor != "" {
		preferred, err := preferredElsewhere(tx, who, anchor, l.ID, need.Anchor, usable)
		if err != nil || preferred {
			return s, false, err
		}
		c, err := content.Connection(tx, anchor.ID)
		if err != nil {
			return s, false, err
		}
		s.members[anchor.Service] = c
		s.NeedsToken = anchor.Mode == enums.CredentialPersonal && !anchor.HasMine
	}
	for i, service := range slices.Concat(need.Required, need.Optional) {
		c, err := member(tx, who, anchor, l.ID, service, usable)
		if err != nil {
			return s, false, err
		}
		if c == nil && i < len(need.Required) {
			return s, false, nil
		}
		if c == nil {
			continue
		}
		s.members[service] = c
		v := usable[c.ID]
		s.NeedsToken = s.NeedsToken || (v.Mode == enums.CredentialPersonal && !v.HasMine)
	}
	return s, true, nil
}

// member is the scope's connection of service: the anchor itself, else
// its partner in the Verbund (verbund.Partner); one the user may not use
// counts as none.
func member(tx *sql.Tx, who *access.Principal, anchor connections.View, link int64, service enums.ServiceType, usable map[int64]connections.View) (*model.Connection, error) {
	if service == anchor.Service {
		return content.Connection(tx, anchor.ID)
	}
	c, _, err := verbund.Partner(tx, who, verbund.Asker{SpaceID: anchor.SpaceID, ConnID: anchor.ID, LinkID: link}, service)
	if err != nil || c == nil {
		return nil, err
	}
	if _, ok := usable[c.ID]; !ok {
		return nil, nil
	}
	return c, nil
}

// Pick is the scope with key want, else the only one, else the first;
// open is true when there were several and want named none of them, so
// the page asks.
func Pick(scopes []Scope, want string) (s Scope, open bool) {
	if len(scopes) == 0 {
		return Scope{}, false
	}
	if i := slices.IndexFunc(scopes, func(s Scope) bool { return s.Key == want }); i >= 0 {
		return scopes[i], false
	}
	return scopes[0], len(scopes) > 1
}

// preferredElsewhere reports whether the anchor's Verbund holds a member
// of the category that comes before the anchor's service: that one
// anchors the Verbund's scope, so it is offered once.
func preferredElsewhere(tx *sql.Tx, who *access.Principal, anchor connections.View, link int64, c caps.Category, usable map[int64]connections.View) (bool, error) {
	for _, m := range caps.Members(c) {
		if m == anchor.Service {
			return false, nil
		}
		other, err := member(tx, who, anchor, link, m, usable)
		if err != nil || other != nil {
			return other != nil, err
		}
	}
	return false, nil
}
