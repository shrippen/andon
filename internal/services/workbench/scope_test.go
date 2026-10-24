package workbench_test

import (
	"strconv"
	"testing"

	"andon/internal/caps"
	"andon/internal/enums"
	"andon/internal/services/verbund"
	"andon/internal/services/workbench"
	"andon/internal/testkit"
)

// One Invoice Ninja in two Verbünde gives two scopes, each with its own
// Sure; one in none is a scope of its own; a scope lacking a required
// service is left out. Without a pick the first is shown and the page
// asks.
func TestScopes(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	conn := func(s enums.ServiceType) int64 { return testkit.Conn(t, d, who, space, s, "demo://"+string(s)) }
	studio, verein, solo := conn(enums.ServiceInvoiceNinja), conn(enums.ServiceSure), conn(enums.ServiceInvoiceNinja)
	other := conn(enums.ServiceSure)
	for name, members := range map[string][]int64{"Studio": {studio, verein}, "Verein": {studio, other}} {
		if _, err := verbund.Create(d, who, name, members, ""); err != nil {
			t.Fatal(err)
		}
	}

	need := workbench.Need{Required: []enums.ServiceType{enums.ServiceInvoiceNinja}, Optional: []enums.ServiceType{enums.ServiceSure}}
	scopes, err := workbench.Scopes(d, who, need)
	if err != nil {
		t.Fatal(err)
	}
	sure := map[string]int64{}
	for _, s := range scopes {
		sure[s.Name] = 0
		if c := s.Conn(enums.ServiceSure); c != nil {
			sure[s.Name] = c.ID
		}
	}
	if len(scopes) != 3 || sure["Studio"] != verein || sure["Verein"] != other {
		t.Fatalf("scopes: %+v", sure)
	}
	lone := scopes[2]
	if lone.Key != "c"+strconv.FormatInt(solo, 10) || lone.Link != 0 || lone.Conn(enums.ServiceInvoiceNinja).ID != solo {
		t.Errorf("lone Ninja: %+v", lone)
	}

	if s, open := workbench.Pick(scopes, ""); !open || s.Key != scopes[0].Key {
		t.Errorf("no pick: %s open %v", s.Key, open)
	}
	if s, open := workbench.Pick(scopes, scopes[1].Key); open || s.Key != scopes[1].Key {
		t.Errorf("pick: %s open %v", s.Key, open)
	}

	paperless := workbench.Need{Required: []enums.ServiceType{enums.ServiceInvoiceNinja, enums.ServicePaperless}}
	if scopes, err := workbench.Scopes(d, who, paperless); err != nil || len(scopes) != 0 {
		t.Errorf("without Paperless: %+v %v", scopes, err)
	}
}

// A category anchor: Snipe-IT alone and Kept alone each make a scope; a
// Verbund with both is offered once, by Kept (the preferred member).
func TestScopesAnchorOnCategory(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	conn := func(s enums.ServiceType) int64 { return testkit.Conn(t, d, who, space, s, "demo://"+string(s)) }
	snipe, kept, ninja := conn(enums.ServiceSnipeIT), conn(enums.ServiceKept), conn(enums.ServiceInvoiceNinja)
	lone := conn(enums.ServiceSnipeIT)
	if _, err := verbund.Create(d, who, "Umzug", []int64{snipe, kept, ninja}, ""); err != nil {
		t.Fatal(err)
	}

	need := workbench.Need{Anchor: caps.Inventory, Optional: []enums.ServiceType{enums.ServiceInvoiceNinja}}
	scopes, err := workbench.Scopes(d, who, need)
	if err != nil {
		t.Fatal(err)
	}
	if len(scopes) != 2 {
		t.Fatalf("scopes: %+v", scopes)
	}
	moving, alone := scopes[0], scopes[1]
	if moving.Name == "Umzug" {
		if c := moving.Of(caps.Inventory); c == nil || c.ID != kept || moving.Conn(enums.ServiceInvoiceNinja).ID != ninja {
			t.Errorf("Verbund: %+v", moving)
		}
	} else {
		moving, alone = alone, moving
	}
	if c := alone.Of(caps.Inventory); c == nil || c.ID != lone {
		t.Errorf("lone Snipe-IT: %+v", alone)
	}
}
