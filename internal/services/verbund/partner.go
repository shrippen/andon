package verbund

import (
	"errors"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	linkrepo "andon/internal/repos/links"
	"andon/internal/services/access"
)

// ErrAmbiguous: several connections fit and none was chosen; a catalog
// key, shown where the partner's data would be.
var ErrAmbiguous = errors.New("verbund.ambiguous")

// PartnerState says how Partner ended.
type PartnerState string

const (
	PartnerFound     PartnerState = "found"
	PartnerNone      PartnerState = "none"      // no connection of the service
	PartnerAmbiguous PartnerState = "ambiguous" // several, none chosen
)

// Asker is who needs a partner: a tile or connection in a space, with
// the Verbund chosen at it (0 = none).
type Asker struct {
	SpaceID int64
	ConnID  int64
	LinkID  int64
}

// Partner finds the connection of service the asker works with:
//
//  1. the member of the Verbund chosen at the asker
//  2. the member of the one Verbund the asker's connection is in
//  3. the one free connection of the service in the asker's space
//  4. the one free connection of the service the user reaches
//
// "Free" leaves out connections that are in a Verbund with another
// connection of the asker's service: Kimai A, linked with Dawarich 1, is
// no candidate for Dawarich 2.
func Partner(q db.Queryer, who *access.Principal, at Asker, service enums.ServiceType) (*model.Connection, PartnerState, error) {
	all, err := linkrepo.All(q)
	if err != nil {
		return nil, PartnerNone, err
	}
	reach := func(c *model.Connection) bool {
		_, ok := who.Spaces[c.SpaceID]
		return ok
	}

	// 1 and 2: stored Verbünde. A member the user cannot reach is no
	// partner, and no reason to pick another connection instead.
	if at.LinkID != 0 && linkHas(all, at.LinkID, service) {
		c, ok, err := chosen(q, all, at.LinkID, service, reach)
		if err != nil || !ok {
			return nil, PartnerNone, err
		}
		return c, PartnerFound, nil
	}
	if at.ConnID != 0 {
		var found []*model.Connection
		linked := false
		for _, l := range all {
			if !hasConn(l, at.ConnID) || !linkHas(all, l.ID, service) {
				continue
			}
			linked = true
			c, ok, err := chosen(q, all, l.ID, service, reach)
			if err != nil {
				return nil, PartnerNone, err
			}
			if ok {
				found = append(found, c)
			}
		}
		if linked {
			c, state := single(found)
			return c, state, nil
		}
	}

	// 3 and 4: the implicit Verbund, own space first.
	askerService, err := serviceOf(q, at.ConnID)
	if err != nil {
		return nil, PartnerNone, err
	}
	taken := takenFor(all, askerService, at.ConnID)
	inSpace, err := candidates(q, []int64{at.SpaceID}, service, taken)
	if err != nil {
		return nil, PartnerNone, err
	}
	if c, state := single(inSpace); state != PartnerNone {
		return c, state, nil
	}
	var others []int64
	for id := range who.Spaces {
		if id != at.SpaceID {
			others = append(others, id)
		}
	}
	reached, err := candidates(q, others, service, taken)
	if err != nil {
		return nil, PartnerNone, err
	}
	c, state := single(reached)
	return c, state, nil
}

// chosen is the reachable member of service in the Verbund id.
func chosen(q db.Queryer, all []linkrepo.Link, id int64, service enums.ServiceType, reach func(*model.Connection) bool) (*model.Connection, bool, error) {
	for _, l := range all {
		if l.ID != id {
			continue
		}
		for _, m := range l.Members {
			if m.Service != string(service) {
				continue
			}
			c, err := content.Connection(q, m.ConnID)
			if err != nil || c == nil || !reach(c) {
				return nil, false, err
			}
			return c, true, nil
		}
	}
	return nil, false, nil
}

// linkHas reports whether the Verbund id has a member of service.
func linkHas(all []linkrepo.Link, id int64, service enums.ServiceType) bool {
	for _, l := range all {
		if l.ID != id {
			continue
		}
		for _, m := range l.Members {
			if m.Service == string(service) {
				return true
			}
		}
	}
	return false
}

// single is the one connection of list; several are ambiguous.
func single(list []*model.Connection) (*model.Connection, PartnerState) {
	switch len(list) {
	case 0:
		return nil, PartnerNone
	case 1:
		return list[0], PartnerFound
	}
	return nil, PartnerAmbiguous
}

// serviceOf is a connection's service, "" for none.
func serviceOf(q db.Queryer, connID int64) (string, error) {
	if connID == 0 {
		return "", nil
	}
	c, err := content.Connection(q, connID)
	if err != nil || c == nil {
		return "", err
	}
	return c.Service, nil
}

// takenFor lists the connections that are in a Verbund with another
// connection of askerService than the asker's own.
func takenFor(all []linkrepo.Link, askerService string, askerConn int64) map[int64]bool {
	taken := map[int64]bool{}
	if askerService == "" {
		return taken
	}
	for _, l := range all {
		other := false
		for _, m := range l.Members {
			if m.Service == askerService && m.ConnID != askerConn {
				other = true
			}
		}
		if !other {
			continue
		}
		for _, m := range l.Members {
			taken[m.ConnID] = true
		}
	}
	return taken
}

// candidates are the connections of service in spaces, without taken.
func candidates(q db.Queryer, spaces []int64, service enums.ServiceType, taken map[int64]bool) ([]*model.Connection, error) {
	if len(spaces) == 0 {
		return nil, nil
	}
	list, err := content.Connections(q, spaces)
	if err != nil {
		return nil, err
	}
	var out []*model.Connection
	for _, c := range list {
		if c.Service == string(service) && !taken[c.ID] {
			out = append(out, c)
		}
	}
	return out, nil
}

// Pair is a connection of one service with its partner of another.
type Pair struct {
	A, B *model.Connection
}

// Pairs pairs every connection of service a in spaces with its partner
// of service b (Partner); connections without one, or with several to
// choose from, are left out.
func Pairs(q db.Queryer, who *access.Principal, spaces []int64, a, b enums.ServiceType) ([]Pair, error) {
	list, err := content.Connections(q, spaces)
	if err != nil {
		return nil, err
	}
	var out []Pair
	for _, c := range list {
		if c.Service != string(a) {
			continue
		}
		partner, state, err := Partner(q, who, Asker{SpaceID: c.SpaceID, ConnID: c.ID}, b)
		if err != nil {
			return nil, err
		}
		if state == PartnerFound {
			out = append(out, Pair{A: c, B: partner})
		}
	}
	return out, nil
}
