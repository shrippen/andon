package hosts_test

import (
	"context"
	"errors"
	"testing"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/rules"
	"andon/internal/services/hints"
	"andon/internal/services/hosts"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
	"andon/internal/testkit"
)

const (
	// healthyURL serves the demo dataset on host "studio".
	healthyURL = "demo://studio"
	// brokenURL points at a closed local port on host "127.0.0.1".
	brokenURL = "http://127.0.0.1:1"
)

// Connections group by host name; hosts with failed services come
// first, lookups ignore case, strangers see nothing.
func TestListGroupsConnectionsByHost(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	stranger, _ := testkit.User(t, d, "other@x.de", enums.RoleUser)
	ctx := context.Background()

	// Seed the stored results the background run would leave behind.
	seeds := []struct {
		service enums.ServiceType
		url     string
	}{
		{enums.ServiceKimai, healthyURL},
		{enums.ServiceInvoiceNinja, healthyURL},
		{enums.ServiceKimai, brokenURL},
	}
	for _, s := range seeds {
		id := testkit.Conn(t, d, who, space, s.service, s.url)
		conn, err := content.Connection(d, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svcdata.Get(ctx, d, sources.DataKey(s.service), nil, conn, model.UserHolder(who.UserID), svcdata.Force); err != nil {
			t.Fatalf("seed %s: %v", s.url, err)
		}
	}

	list, err := hosts.List(ctx, d, who)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("hosts: %+v", list)
	}
	broken, healthy := list[0], list[1]
	if broken.Name != "127.0.0.1" || broken.Problems != 1 || len(broken.Services) != 1 || broken.Services[0].OK {
		t.Fatalf("broken host first: %+v", broken)
	}
	if healthy.Name != "studio" || healthy.Problems != 0 || len(healthy.Services) != 2 {
		t.Fatalf("healthy host: %+v", healthy)
	}

	one, err := hosts.One(ctx, d, who, "STUDIO")
	if err != nil || one.Name != "studio" || len(one.Services) != 2 {
		t.Fatalf("one: %+v, %v", one, err)
	}
	if _, err := hosts.One(ctx, d, who, "nas.lan"); !errors.Is(err, hosts.ErrNotFound) {
		t.Fatalf("unknown host: %v", err)
	}

	if other, _ := hosts.List(ctx, d, stranger); len(other) != 0 {
		t.Fatalf("stranger sees hosts: %+v", other)
	}
}

// TestHostAlertsAndBeats: a firing Prometheus alert lands on the host of
// its instance label, a heartbeat on the host its tag names; both count
// as problems when bad.
func TestHostAlertsAndBeats(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	ctx := context.Background()
	for _, s := range []struct {
		service enums.ServiceType
		url     string
	}{{enums.ServiceKimai, "demo://nas"}, {enums.ServicePrometheus, "demo://prom"}, {enums.ServiceHealthchecks, "demo://hc"}} {
		conn, err := content.Connection(d, testkit.Conn(t, d, who, space, s.service, s.url))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svcdata.Get(ctx, d, sources.DataKey(s.service), nil, conn, model.UserHolder(who.UserID), svcdata.Force); err != nil {
			t.Fatalf("seed %s: %v", s.service, err)
		}
	}

	nas, err := hosts.One(ctx, d, who, "nas")
	if err != nil {
		t.Fatal(err)
	}
	if len(nas.Alerts) != 1 || nas.Alerts[0].Name != "HostHighCpuLoad" || len(nas.Beats) != 1 || nas.Beats[0].Name != "borg-nas" || nas.Problems != 1 {
		t.Fatalf("nas: alerts %+v beats %+v problems %d", nas.Alerts, nas.Beats, nas.Problems)
	}
}

// TestHostCVEs: a CVE that hits an image of a compose stack lands on the
// stack's host (same first label); an affected one is a problem.
func TestHostCVEs(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	ctx := context.Background()
	for _, s := range []struct {
		service enums.ServiceType
		url     string
	}{{enums.ServiceKimai, "demo://nebelhorn.lan"}, {enums.ServiceGitea, "demo://git"}, {enums.ServiceNVD, "demo://nvd"}} {
		conn, err := content.Connection(d, testkit.Conn(t, d, who, space, s.service, s.url))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svcdata.Get(ctx, d, sources.DataKey(s.service), nil, conn, model.UserHolder(who.UserID), svcdata.Force); err != nil {
			t.Fatalf("seed %s: %v", s.service, err)
		}
	}
	h, err := hosts.One(ctx, d, who, "nebelhorn.lan")
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, m := range h.CVEs {
		ids[m.CVE.ID] = true
	}
	if !ids["CVE-2026-41207"] || ids["CVE-2026-39954"] || h.Problems < 1 {
		t.Fatalf("cves %+v problems %d", h.CVEs, h.Problems)
	}
}

// TestHostHints: a hint lands on the host of the item it names (host,
// node, guest, device; by full name or first label), else on its
// connection's host. It counts as a problem unless it mirrors a check
// the host page already counts (failed connection). Hosts without
// monitor, hint or problem are quiet.
func TestHostHints(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	ctx := context.Background()

	conns := map[string]int64{}
	for _, url := range []string{"demo://nas.lan", "demo://studio", "demo://quiet"} {
		id := testkit.Conn(t, d, who, space, enums.ServiceKimai, url)
		conn, err := content.Connection(d, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceKimai), nil, conn, model.UserHolder(who.UserID), svcdata.Force); err != nil {
			t.Fatalf("seed %s: %v", url, err)
		}
		conns[url] = id
	}

	// All raised by the connection on "studio".
	studio := conns["demo://studio"]
	findings := []rules.Finding{
		{Fingerprint: "a", Rule: "certs.expiring", Severity: enums.SeverityWarn, Message: "certs.expiring", Params: map[string]any{"host": "NAS.lan"}},
		{Fingerprint: "b", Rule: "proxmox.backup_old", Severity: enums.SeverityWarn, Message: "proxmox.backup_old", Params: map[string]any{"guest": "nas", "vmid": "101"}},
		{Fingerprint: "c", Rule: "kimai.plain", Severity: enums.SeverityInfo, Message: "kimai.plain", Params: map[string]any{"guest": "elsewhere"}},
		{Fingerprint: "d", Rule: "system.connector_down", Severity: enums.SeverityCritical, Message: "system.connector_down", Params: map[string]any{"name": "kimai"}},
	}
	ruleIDs := []string{"certs.expiring", "proxmox.backup_old", "kimai.plain", "system.connector_down"}
	if _, err := hints.Sync(d, space, nil, &studio, ruleIDs, findings); err != nil {
		t.Fatal(err)
	}

	list, err := hosts.List(ctx, d, who)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]hosts.Host{}
	for _, h := range list {
		byName[h.Name] = h
	}
	if h := byName["nas.lan"]; len(h.Hints) != 2 || h.Problems != 2 {
		t.Fatalf("nas.lan: hints %d problems %d", len(h.Hints), h.Problems)
	}
	if h := byName["studio"]; len(h.Hints) != 2 || h.Problems != 1 {
		t.Fatalf("studio: hints %d problems %d", len(h.Hints), h.Problems)
	}
	if list[0].Name != "nas.lan" {
		t.Fatalf("most problems first: %s", list[0].Name)
	}

	found, quiet := hosts.Split(list)
	if len(found) != 2 || len(quiet) != 1 || quiet[0].Name != "quiet" {
		t.Fatalf("found %d, quiet %+v", len(found), quiet)
	}

	one, err := hosts.One(ctx, d, who, "nas.lan")
	if err != nil || len(one.Hints) != 2 {
		t.Fatalf("one: %d hints, %v", len(one.Hints), err)
	}
}
