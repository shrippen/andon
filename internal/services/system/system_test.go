package system_test

import (
	"errors"
	"testing"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/services/audit"
	"andon/internal/services/system"
	"andon/internal/testkit"
)

// Only admins change instance settings; a bad network is refused whole.
func TestSettingsAdminOnly(t *testing.T) {
	d := testkit.DB(t)
	admin, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	user, _ := testkit.User(t, d, "user@x.de", enums.RoleUser)
	t.Cleanup(func() { _ = system.SetNetwork(d, admin, system.NetworkPolicy{Mode: system.NetOpen, Public: true}, "") })

	if err := system.Put(d, user, system.SecurityKey, map[string]any{"force_admin_totp": true}, ""); !errors.Is(err, system.ErrDenied) {
		t.Fatalf("user put: %v", err)
	}
	if err := system.Put(d, admin, system.SecurityKey, map[string]any{"force_admin_totp": true}, ""); err != nil {
		t.Fatal(err)
	}
	if !system.Flag(d, system.SecurityKey, "force_admin_totp") {
		t.Fatal("flag not stored")
	}

	bad := system.NetworkPolicy{Mode: system.NetAllowlist, Networks: []string{"not a cidr"}}
	if err := system.SetNetwork(d, admin, bad, ""); err == nil {
		t.Fatal("bad CIDR accepted")
	}
	good := system.NetworkPolicy{Mode: system.NetAllowlist, Networks: []string{"192.168.10.0/24"}, Hosts: []string{"nas.lan"}}
	if err := system.SetNetwork(d, admin, good, ""); err != nil {
		t.Fatal(err)
	}
	got, err := system.Network(d)
	if err != nil || got.Mode != system.NetAllowlist || got.Public || len(got.Networks) != 1 || got.Hosts[0] != "nas.lan" {
		t.Fatalf("network: %+v %v", got, err)
	}
}

// Health shows admins what the Pi is busy with: jobs, database, cache,
// memory. Other users see nothing.
func TestHealthAdminOnly(t *testing.T) {
	d := testkit.DB(t)
	admin, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	user, _ := testkit.User(t, d, "user@x.de", enums.RoleUser)

	if _, err := system.Health(d, user); !errors.Is(err, system.ErrDenied) {
		t.Fatalf("user: %v", err)
	}
	h, err := system.Health(d, admin)
	if err != nil {
		t.Fatal(err)
	}
	if h.DatabaseMB <= 0 || h.HeapMB <= 0 || h.Goroutines == 0 {
		t.Fatalf("health: %+v", h)
	}
}

// Saving settings logs only what changed, with the old and new value;
// saving the same values again logs nothing.
func TestPutAuditsChanges(t *testing.T) {
	d := testkit.DB(t)
	admin, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)

	for range 2 {
		if err := system.Put(d, admin, system.RegistrationKey, map[string]any{"open": true}, ""); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := audit.Entries(d, admin)
	if err != nil {
		t.Fatal(err)
	}
	var logged []*model.AuditEntry
	for _, e := range entries {
		if e.Action == "settings."+system.RegistrationKey {
			logged = append(logged, e)
		}
	}
	if len(logged) != 1 {
		t.Fatalf("entries: %d", len(logged))
	}
	change, _ := logged[0].Detail["open"].([]any)
	if len(change) != 2 || change[0] != nil || change[1] != true {
		t.Fatalf("detail: %+v", logged[0].Detail)
	}
}
