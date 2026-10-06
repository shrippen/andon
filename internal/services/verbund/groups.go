package verbund

import (
	"andon/internal/caps"
	"andon/internal/enums"
	"slices"
	"strings"

	"andon/internal/model"
	linkrepo "andon/internal/repos/links"
)

// Group is the connections whose datasets the analysis reads together:
// one per service.
type Group struct {
	LinkID int64 // 0 = the space's implicit Verbund
	Conns  map[string]*model.Connection
	// Fan holds the free connections of services that pair with nothing
	// (caps.Paired) and run several times in the space, e.g. Docker
	// hosts: the cross checks read every one of them.
	Fan map[string][]*model.Connection
}

// Groups splits a space's connections into the Verbünde its rules run
// in, for the analysis (no user, no rights: only what lives in this
// space, so no other space's data reaches its hints):
//
//	stored Verbund with a member here ─► its members here
//	                                     + the space's unambiguous others
//	the rest ──────────────────────────► the implicit Verbund
//
// Unambiguous is the one connection of a service that is in no stored
// Verbund ("free"), else the only one there is. Ambiguous lists paired
// services with several free connections; they join no group by
// themselves. Several free ones of other services go to every group's
// Fan.
func Groups(conns []*model.Connection, all []linkrepo.Link) ([]Group, []string) {
	here := map[int64]*model.Connection{}
	for _, c := range conns {
		here[c.ID] = c
	}

	// Stored Verbünde with members here, and which connections they hold.
	linked := map[int64]bool{}
	var stored []linkrepo.Link
	for _, l := range all {
		if !slices.ContainsFunc(l.Members, func(m linkrepo.Member) bool { return here[m.ConnID] != nil }) {
			continue
		}
		stored = append(stored, l)
		for _, m := range l.Members {
			linked[m.ConnID] = true
		}
	}

	free, only, fan, ambiguous := unambiguous(conns, linked)
	var out []Group
	for _, l := range stored {
		g := Group{LinkID: l.ID, Conns: map[string]*model.Connection{}, Fan: fan}
		for _, m := range l.Members {
			if c := here[m.ConnID]; c != nil {
				g.Conns[c.Service] = c
			}
		}
		fillGroup(g, free, only, stored)
		out = append(out, g)
	}

	// The implicit Verbund runs when it holds a free connection, or when
	// there is no stored one at all (a space without Verbünde: as before).
	if len(free) > 0 || len(fan) > 0 || len(stored) == 0 {
		implicit := Group{Conns: map[string]*model.Connection{}, Fan: fan}
		fillGroup(implicit, free, only, stored)
		out = append(out, implicit)
	}
	return out, ambiguous
}

// fillGroup adds the free connections, then the only ones whose own
// Verbünde agree with the group: Dawarich 1, linked with Kimai 1, does
// not join a group that has Kimai 2.
func fillGroup(g Group, free, only map[string]*model.Connection, stored []linkrepo.Link) {
	for s, c := range free {
		if _, ok := g.Conns[s]; !ok {
			g.Conns[s] = c
		}
	}
	for s, c := range only {
		if _, ok := g.Conns[s]; ok || !agrees(g, c, stored) {
			continue
		}
		g.Conns[s] = c
	}
}

// agrees reports whether every Verbund of c holds, for each service the
// group has, the group's own connection.
func agrees(g Group, c *model.Connection, stored []linkrepo.Link) bool {
	for _, l := range stored {
		if !slices.ContainsFunc(l.Members, func(m linkrepo.Member) bool { return m.ConnID == c.ID }) {
			continue
		}
		for _, m := range l.Members {
			if have, ok := g.Conns[m.Service]; ok && have.ID != m.ConnID {
				return false
			}
		}
	}
	return true
}

// unambiguous picks per service the one free connection, else the only
// (linked) one; services with several free ones are ambiguous.
func unambiguous(conns []*model.Connection, linked map[int64]bool) (one, only map[string]*model.Connection, fan map[string][]*model.Connection, ambiguous []string) {
	free := map[string][]*model.Connection{}
	total := map[string][]*model.Connection{}
	for _, c := range conns {
		total[c.Service] = append(total[c.Service], c)
		if !linked[c.ID] {
			free[c.Service] = append(free[c.Service], c)
		}
	}

	one, only, fan = map[string]*model.Connection{}, map[string]*model.Connection{}, map[string][]*model.Connection{}
	for s, list := range total {
		switch {
		case len(free[s]) == 1:
			one[s] = free[s][0]
		case len(free[s]) > 1 && !caps.Paired(enums.ServiceType(s)):
			fan[s] = free[s]
		case len(free[s]) > 1:
			ambiguous = append(ambiguous, s)
		case len(list) == 1:
			only[s] = list[0]
		}
	}
	slices.SortFunc(ambiguous, strings.Compare)
	return one, only, fan, ambiguous
}
