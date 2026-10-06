package receipts_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/services/receipts"
	"andon/internal/services/verbund"
	"andon/internal/testkit"
)

// One Ninja, two Paperless: the receipts page asks, until a Verbund
// names the Ninja's Paperless.
func TestSetupFromVerbund(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	ninja := testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, "https://n.example")
	testkit.Conn(t, d, who, space, enums.ServicePaperless, "https://p1.example")
	p2 := testkit.Conn(t, d, who, space, enums.ServicePaperless, "https://p2.example")

	s, err := receipts.SetupOf(d, who)
	if err != nil || !s.Pick || s.Paperless != 0 || s.Ninja != ninja {
		t.Fatalf("before: %+v %v", s, err)
	}
	if _, err := verbund.Create(d, who, "Büro", []int64{ninja, p2}, ""); err != nil {
		t.Fatal(err)
	}
	s, err = receipts.SetupOf(d, who)
	if err != nil || s.Pick || s.Paperless != p2 || s.Ninja != ninja {
		t.Fatalf("after: %+v %v", s, err)
	}
}
