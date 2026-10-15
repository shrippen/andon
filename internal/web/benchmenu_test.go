package web

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/services/hints"
	"andon/internal/services/workbench"
)

// TestBenchMenu: without stars the benches with open hints are the
// favourites; once chosen only the starred ones, open hints or not.
func TestBenchMenu(t *testing.T) {
	tally := hints.Tally{Pages: map[string]hints.PageCount{"/receipts": {N: 3, Level: enums.SeverityWarn}}}

	groups, favs := benchMenu(tally, workbench.Favs{})
	if len(groups) != len(benchGroups) || len(favs) != 1 || favs[0].ID != "receipts" || favs[0].N != 3 {
		t.Fatalf("default %+v", favs)
	}

	_, favs = benchMenu(tally, workbench.Favs{Chosen: true, IDs: []string{"vat"}})
	if len(favs) != 1 || favs[0].ID != "vat" {
		t.Fatalf("chosen %+v", favs)
	}
	if _, favs = benchMenu(tally, workbench.Favs{Chosen: true}); len(favs) != 0 {
		t.Fatalf("none chosen %+v", favs)
	}
}
