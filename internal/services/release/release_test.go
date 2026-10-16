package release

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/testkit"
)

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		latest, running string
		want            bool
	}{
		{"0.10.0", "0.9.3", true},
		{"0.9.3", "0.10.0", false},
		{"0.8", "0.7.9", true},
		{"0.7.0", "0.7", false},
		{"0.7.1", "v0.7.0", true},
		{"0.8.0", "0.7.0/main/#12", true},
		{"0.7.0", "0.7.0/main/#12", false},
		{"0.8.0-rc1", "0.7.0", true},
		{"0.8.0", "dev", false},
		{"", "0.7.0", false},
	} {
		if got := Newer(c.latest, c.running); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.latest, c.running, got, c.want)
		}
	}
}

// stub replaces the fetch and counts the requests.
func stub(t *testing.T, r Release, err error) *int {
	t.Helper()
	calls := 0
	old := fetch
	fetch = func(context.Context, string) (Release, error) {
		calls++
		return r, err
	}
	t.Cleanup(func() { fetch = old })
	return &calls
}

// TestCheckOncePerDay: one request a day, a failed one included.
func TestCheckOncePerDay(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	calls := stub(t, Release{}, errors.New("offline"))

	if err := Check(context.Background(), dir, now); err == nil {
		t.Fatal("failure not reported")
	}
	if err := Check(context.Background(), dir, now.Add(time.Hour)); err != nil || *calls != 1 {
		t.Fatalf("second try within a day: calls %d, err %v", *calls, err)
	}
	if _, ok := cached(dir); ok {
		t.Fatal("a failed try left a release")
	}

	*calls = 0
	fetch = func(context.Context, string) (Release, error) {
		*calls++
		return Release{Version: "0.8.0", Date: "2026-10-20", URL: "https://example.org/r"}, nil
	}
	if err := Check(context.Background(), dir, now.Add(Interval+time.Minute)); err != nil || *calls != 1 {
		t.Fatalf("next day: calls %d, err %v", *calls, err)
	}
	if r, ok := cached(dir); !ok || r.Version != "0.8.0" {
		t.Fatalf("cached = %+v, %v", r, ok)
	}
}

// TestPending: admins only, newer than the build, until they hide it.
func TestPending(t *testing.T) {
	d := testkit.DB(t)
	dir := t.TempDir()
	admin, _ := testkit.User(t, d, "admin@x.y", enums.RoleAdmin)
	member, _ := testkit.User(t, d, "member@x.y", enums.RoleUser)
	if err := os.WriteFile(filepath.Join(dir, cacheName), []byte(`{"version":"0.8.0","date":"2026-10-20","url":"https://example.org/r"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	old := running
	running = func() string { return "0.7.0" }
	t.Cleanup(func() { running = old })

	if r, ok := Pending(d, admin, dir); !ok || r.Version != "0.8.0" {
		t.Fatalf("admin: %+v, %v", r, ok)
	}
	if _, ok := Pending(d, member, dir); ok {
		t.Fatal("member sees the hint")
	}
	if err := Hide(d, member, "0.8.0"); err == nil {
		t.Fatal("member may hide")
	}

	if err := Hide(d, admin, "0.8.0"); err != nil {
		t.Fatal(err)
	}
	if _, ok := Pending(d, admin, dir); ok {
		t.Fatal("hidden hint still shown")
	}

	// The next version shows again.
	if err := os.WriteFile(filepath.Join(dir, cacheName), []byte(`{"version":"0.9.0","date":"2026-11-02","url":"https://example.org/r"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if r, ok := Pending(d, admin, dir); !ok || r.Version != "0.9.0" {
		t.Fatalf("next version: %+v, %v", r, ok)
	}

	running = func() string { return "0.9.0" }
	if _, ok := Pending(d, admin, dir); ok {
		t.Fatal("hint for the running version")
	}
}
