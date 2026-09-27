package hooks_test

import (
	"errors"
	"path"
	"strconv"
	"testing"

	"andon/internal/enums"
	"andon/internal/services/hooks"
	"andon/internal/testkit"
)

// Events land only with the right signature and on push services.
func TestReceiveChecksSignatureAndService(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	pg := testkit.Conn(t, d, who, space, enums.ServicePGBackWeb, "https://pg.example")
	kimai := testkit.Conn(t, d, who, space, enums.ServiceKimai, "https://kimai.example")

	url, err := hooks.URL(d, "https://dash.example/", pg)
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://dash.example/hooks/" + strconv.FormatInt(pg, 10) + "/"; url[:len(want)] != want {
		t.Fatalf("url %q", url)
	}
	sig := path.Base(url)

	if err := hooks.Receive(d, pg, sig, "backup.failed", "db"); err != nil {
		t.Fatalf("receive: %v", err)
	}
	if err := hooks.Receive(d, pg, sig+"x", "backup.failed", "db"); !errors.Is(err, hooks.ErrRejected) {
		t.Fatalf("bad signature: %v", err)
	}
	kimaiURL, _ := hooks.URL(d, "https://dash.example", kimai)
	if err := hooks.Receive(d, kimai, path.Base(kimaiURL), "x", "y"); !errors.Is(err, hooks.ErrRejected) {
		t.Fatalf("non-push service: %v", err)
	}
}

// A leaked URL is revoked by rotating: the old signature stops working.
func TestRotateRevokesOldURL(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	pg := testkit.Conn(t, d, who, space, enums.ServicePGBackWeb, "https://pg.example")

	old, _ := hooks.URL(d, "https://dash.example", pg)
	if err := hooks.Rotate(d, pg); err != nil {
		t.Fatal(err)
	}
	fresh, _ := hooks.URL(d, "https://dash.example", pg)
	if fresh == old {
		t.Fatal("rotation kept the URL")
	}
	if err := hooks.Receive(d, pg, path.Base(old), "backup.failed", "db"); !errors.Is(err, hooks.ErrRejected) {
		t.Fatalf("old URL: %v", err)
	}
	if err := hooks.Receive(d, pg, path.Base(fresh), "backup.failed", "db"); err != nil {
		t.Fatalf("new URL: %v", err)
	}
}

// A flood through a valid URL is cut off instead of filling the table.
func TestReceiveThrottles(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	pg := testkit.Conn(t, d, who, space, enums.ServicePGBackWeb, "https://pg.example")
	url, _ := hooks.URL(d, "https://dash.example", pg)

	var err error
	for range hooks.PerMinute + 1 {
		err = hooks.Receive(d, pg, path.Base(url), "backup.failed", "db")
	}
	if !errors.Is(err, hooks.ErrThrottled) {
		t.Fatalf("expected throttling, got %v", err)
	}
}
