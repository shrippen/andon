package workbench_test

import (
	"strconv"
	"testing"

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
