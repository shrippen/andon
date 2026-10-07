//go:build !release

package seed_test

import (
	"context"
	"path/filepath"
	"testing"

	"andon/internal/crypto"
	"andon/internal/db"
	"andon/internal/db/dbtest"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/users"
	"andon/internal/services/access"
	"andon/internal/services/boards"
	"andon/internal/services/connections"
	"andon/internal/services/hints"
	"andon/internal/services/reports"
	"andon/internal/services/seed"
	"andon/internal/services/system"
)

// TestDemoFillsBoardsAndFiresRules: the demo instance imports every widget
// and the analysis turns the demo datasets into hints.
func TestDemoFillsBoardsAndFiresRules(t *testing.T) {
	crypto.Init(crypto.Derive("test-master-key", nil))
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"), dbtest.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := system.Start(d); err != nil {
		t.Fatal(err)
	}

	if err := seed.Demo(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	user, _ := users.ByEmail(d, seed.DemoUser)
	who, _ := access.Load(d, user.ID)

	visible, _ := boards.Visible(d, who)
	if len(visible) < 5 {
		t.Fatalf("expected personal and instance boards, got %d", len(visible))
	}
	found, err := hints.Active(d, who, enums.SeverityInfo, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	rules := map[string]bool{}
	for _, h := range found {
		rules[h.Rule] = true
	}
	for _, want := range []string{"kimai.unbilled_hours", "in.invoice_overdue", "snipe.warranty_expiring", "geo.visit_without_time",
		"cross.ci_red_deployed", "cross.pbs_pool", "cross.ups_load"} {
		if !rules[want] {
			t.Errorf("expected hint %s from the demo data, got %v", want, rules)
		}
	}

	// The router's reconnects of the last weeks are in the history: the
	// ISP report lists them, two with a downtime a run saw.
	isp, err := reports.ISPReports(context.Background(), d, who)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, r := range isp {
		for _, c := range r.Reconnects {
			if c.Seen {
				seen++
			}
		}
	}
	if len(isp) != 1 || len(isp[0].Reconnects) < 5 || seen != 2 {
		t.Errorf("isp: %+v", isp)
	}

	// Templates: the user's Nextcloud login is current, Immich's paused.
	activations, err := connections.Activations(d, who, model.UserHolder(user.ID))
	if err != nil {
		t.Fatal(err)
	}
	state := map[enums.ServiceType]string{}
	for _, a := range activations {
		state[a.Template.Service] = map[bool]string{true: "active", false: "paused"}[a.Active]
	}
	if state[enums.ServiceNextcloud] != "active" || state[enums.ServiceImmich] != "paused" {
		t.Errorf("demo templates: %v", state)
	}

	if err := seed.Demo(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	if n, _ := users.Count(d); n != 2 {
		t.Fatalf("demo must run only once, got %d users", n)
	}
}
