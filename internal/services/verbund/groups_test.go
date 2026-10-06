package verbund

import (
	"testing"

	"andon/internal/model"
	linkrepo "andon/internal/repos/links"
)

func conn(id int64, service string) *model.Connection {
	return &model.Connection{ID: id, Service: service}
}

func member(c *model.Connection) linkrepo.Member {
	return linkrepo.Member{ConnID: c.ID, Service: c.Service}
}

func ids(g Group) map[string]int64 {
	out := map[string]int64{}
	for s, c := range g.Conns {
		out[s] = c.ID
	}
	return out
}

// Without Verbünde one connection per service is today's behaviour.
func TestGroupsWithoutVerbund(t *testing.T) {
	daw, kimai, ninja := conn(1, "dawarich"), conn(2, "kimai"), conn(3, "invoiceninja")
	groups, amb := Groups([]*model.Connection{daw, kimai, ninja}, nil)
	if len(groups) != 1 || len(groups[0].Conns) != 3 || groups[0].LinkID != 0 || amb != nil {
		t.Fatalf("groups %+v amb %v", groups, amb)
	}
}

// Two Kimai, no Verbund: Kimai joins no group and is named ambiguous.
func TestGroupsAmbiguous(t *testing.T) {
	daw, k1, k2 := conn(1, "dawarich"), conn(2, "kimai"), conn(3, "kimai")
	groups, amb := Groups([]*model.Connection{daw, k1, k2}, nil)
	if len(groups) != 1 || groups[0].Conns["kimai"] != nil || len(amb) != 1 || amb[0] != "kimai" {
		t.Fatalf("groups %+v amb %v", groups, amb)
	}
}

// Two firms: Verbund 1 = Dawarich + Kimai 1, the free Kimai 2 forms the
// implicit one; the one Ninja and Sure serve both.
func TestGroupsTwoFirms(t *testing.T) {
	daw, k1, k2, ninja := conn(1, "dawarich"), conn(2, "kimai"), conn(3, "kimai"), conn(4, "invoiceninja")
	links := []linkrepo.Link{{ID: 7, Members: []linkrepo.Member{member(daw), member(k1)}}}
	groups, amb := Groups([]*model.Connection{daw, k1, k2, ninja}, links)
	if amb != nil || len(groups) != 2 {
		t.Fatalf("groups %+v amb %v", groups, amb)
	}
	if got := ids(groups[0]); groups[0].LinkID != 7 || got["kimai"] != 2 || got["dawarich"] != 1 || got["invoiceninja"] != 4 {
		t.Fatalf("stored %v", got)
	}
	// Dawarich 1 belongs to Kimai 1: it does not join Kimai 2.
	if got := ids(groups[1]); groups[1].LinkID != 0 || got["kimai"] != 3 || got["invoiceninja"] != 4 || got["dawarich"] != 0 {
		t.Fatalf("implicit %v", got)
	}
}

// All connections in Verbünde: no implicit group; members of another
// space stay out.
func TestGroupsAllLinked(t *testing.T) {
	daw, k1 := conn(1, "dawarich"), conn(2, "kimai")
	elsewhere := conn(9, "invoiceninja")
	links := []linkrepo.Link{{ID: 7, Members: []linkrepo.Member{member(daw), member(k1), member(elsewhere)}}}
	groups, _ := Groups([]*model.Connection{daw, k1}, links)
	if len(groups) != 1 || groups[0].LinkID != 7 || groups[0].Conns["invoiceninja"] != nil {
		t.Fatalf("groups %+v", groups)
	}
	// A Verbund without a member here does not run here.
	groups, _ = Groups([]*model.Connection{conn(5, "sure")}, links)
	if len(groups) != 1 || groups[0].LinkID != 0 {
		t.Fatalf("foreign verbund ran: %+v", groups)
	}
}

// The only Ninja is in a Verbund with Kimai 2: it serves Kimai 2's
// group, not Kimai 1's.
func TestGroupsOnlyOneAgrees(t *testing.T) {
	k1, k2, ninja := conn(1, "kimai"), conn(2, "kimai"), conn(3, "invoiceninja")
	daw := conn(4, "dawarich")
	links := []linkrepo.Link{
		{ID: 7, Members: []linkrepo.Member{member(daw), member(k1)}},
		{ID: 8, Members: []linkrepo.Member{member(k2), member(ninja)}},
	}
	groups, _ := Groups([]*model.Connection{k1, k2, ninja, daw}, links)
	if len(groups) != 2 {
		t.Fatalf("groups %+v", groups)
	}
	if got := ids(groups[0]); got["invoiceninja"] != 0 || got["kimai"] != 1 {
		t.Fatalf("firm 1 %v", got)
	}
	if got := ids(groups[1]); got["invoiceninja"] != 3 || got["kimai"] != 2 || got["dawarich"] != 0 {
		t.Fatalf("firm 2 %v", got)
	}
}

// Three Docker hosts and a Kuma, no Verbund: Docker is no partner of
// anything, so it is not ambiguous; every group reads all its hosts.
func TestGroupsSpaceWideServices(t *testing.T) {
	d1, d2, d3, kuma, k1, k2 := conn(1, "docker"), conn(2, "docker"), conn(3, "docker"), conn(4, "uptimekuma"), conn(5, "kimai"), conn(6, "kimai")
	daw := conn(7, "dawarich")
	links := []linkrepo.Link{{ID: 9, Members: []linkrepo.Member{member(daw), member(k1)}}}
	groups, amb := Groups([]*model.Connection{d1, d2, d3, kuma, k1, k2, daw}, links)
	if amb != nil || len(groups) != 2 {
		t.Fatalf("groups %+v amb %v", groups, amb)
	}
	for _, g := range groups {
		if g.Conns["docker"] != nil || len(g.Fan["docker"]) != 3 || g.Conns["uptimekuma"] != kuma {
			t.Fatalf("group %d: conns %v fan %v", g.LinkID, ids(g), g.Fan)
		}
	}

	// Without any Verbund the implicit group still carries them.
	groups, amb = Groups([]*model.Connection{d1, d2}, nil)
	if amb != nil || len(groups) != 1 || len(groups[0].Fan["docker"]) != 2 {
		t.Fatalf("groups %+v amb %v", groups, amb)
	}
}

// A space-wide service someone put into a Verbund pairs like any other.
func TestGroupsSpaceWideInVerbund(t *testing.T) {
	d1, d2, kuma := conn(1, "docker"), conn(2, "docker"), conn(3, "uptimekuma")
	links := []linkrepo.Link{{ID: 4, Members: []linkrepo.Member{member(d1), member(kuma)}}}
	groups, amb := Groups([]*model.Connection{d1, d2, kuma}, links)
	if amb != nil || len(groups) != 2 {
		t.Fatalf("groups %+v amb %v", groups, amb)
	}
	if got := ids(groups[0]); got["docker"] != 1 || len(groups[0].Fan) != 0 {
		t.Fatalf("stored %v fan %v", got, groups[0].Fan)
	}
	if got := ids(groups[1]); got["docker"] != 2 {
		t.Fatalf("implicit %v", got)
	}
}
