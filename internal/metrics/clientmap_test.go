package metrics

import (
	"testing"

	"andon/internal/sources"
)

// A stored link wins over the name; "no counterpart" beats a same name;
// without a link the name decides.
func TestClientMap(t *testing.T) {
	ninja := &sources.NinjaDataset{Clients: []sources.NinjaClient{
		{ID: 1, Key: "Kx9", Name: "ACME GmbH & Co. KG"},
		{ID: 2, Key: "Zz1", Name: "Beta"},
	}}
	m := ClientMap{7: "Kx9", 8: ""}

	if c, ok := m.ClientOf(ninja, 7, "Acme"); !ok || c.ID != 1 {
		t.Fatalf("linked: %+v %v", c, ok)
	}
	if _, ok := m.ClientOf(ninja, 8, "Beta"); ok {
		t.Fatal("no counterpart matched by name")
	}
	if c, ok := m.ClientOf(ninja, 9, " beta "); !ok || c.ID != 2 {
		t.Fatalf("by name: %+v %v", c, ok)
	}
	if _, ok := (ClientMap{7: "gone"}).ClientOf(ninja, 7, "ACME GmbH & Co. KG"); ok {
		t.Fatal("a stale link fell back to the name")
	}
	kimai := &sources.KimaiDataset{Customers: []sources.KimaiCustomer{{ID: 7, Name: "Acme"}, {ID: 8, Name: "Beta"}}}
	if got := m.Customers(kimai, ninja); got[1] != 7 || got[2] != 0 {
		t.Fatalf("customers: %v", got)
	}
}
