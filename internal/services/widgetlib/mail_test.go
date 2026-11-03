package widgetlib_test

import (
	"context"
	"testing"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/services/connections"
	"andon/internal/services/mailfwd"
	"andon/internal/services/svcdata"
	"andon/internal/services/widgetlib"
	"andon/internal/sources"
	"andon/internal/testkit"
)

// The mail invoices tile leaves a mail set aside as no invoice out of
// its count.
func TestMailTileLeavesDismissed(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	box := testkit.Conn(t, d, who, space, enums.ServiceMail, "demo://mail")
	id, err := widgetlib.Create(d, who, space, "mail_invoices", "", nil, &box, nil)
	if err != nil {
		t.Fatal(err)
	}
	w, _, _ := widgetlib.Detail(d, who, id)
	conn, err := connections.ByID(d, box)
	if err != nil {
		t.Fatal(err)
	}
	// The background run's fetch, which tiles read.
	if _, err := svcdata.Get(context.Background(), d, sources.DataKey(enums.ServiceMail), nil, conn, model.NoHolder, svcdata.Force); err != nil {
		t.Fatal(err)
	}
	count := func() any {
		t.Helper()
		frag, err := widgetlib.Load(context.Background(), d, who, w, svcdata.Cached)
		if err != nil {
			t.Fatal(err)
		}
		return frag.View["Count"]
	}

	if n := count(); n != 2 {
		t.Fatalf("before: count %v, want 2", n)
	}
	if err := mailfwd.Dismiss(context.Background(), d, who, box, 2, ""); err != nil {
		t.Fatal(err)
	}
	if n := count(); n != 1 {
		t.Fatalf("after dismissing UID 2: count %v, want 1", n)
	}
}
