package analysis_test

import (
	"context"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/services/analysis"
	"andon/internal/services/mailfwd"
	"andon/internal/testkit"
)

// A mail set aside as no invoice is no unrecorded invoice: its hint goes
// with the next run.
func TestDismissedMailNoHint(t *testing.T) {
	d := testkit.DB(t)
	space := testkit.Instance(t, d)
	who, _ := testkit.User(t, d, "a@x.de", enums.RoleAdmin)
	box := testkit.Conn(t, d, who, space, enums.ServiceMail, "demo://mail")
	testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, "demo://invoiceninja")
	ctx := context.Background()
	open := func() int {
		t.Helper()
		if _, err := analysis.RunAll(ctx, d, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		var n int
		if err := d.QueryRow("SELECT COUNT(*) FROM hints WHERE rule = 'mail.invoice_unrecorded' AND resolved_at IS NULL").Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// Farbraum's mail (UID 2) matches no expense.
	before := open()
	if before == 0 {
		t.Fatal("before: no unrecorded mail")
	}
	if err := mailfwd.Dismiss(ctx, d, who, box, 2, ""); err != nil {
		t.Fatal(err)
	}
	if n := open(); n != before-1 {
		t.Fatalf("after dismissing UID 2: %d unrecorded mails, want %d", n, before-1)
	}
}
