package rules_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

func TestDNSFilterRules(t *testing.T) {
	pihole := sources.DemoPihole(time.Now())
	if got := run(t, "pihole.disabled", pihole, todayEnv(nil)); len(got) != 1 || got[0].Message != "dnsfilter.disabled" {
		t.Fatalf("disabled: %+v", got)
	}
	if got := run(t, "pihole.lists_old", pihole, todayEnv(nil)); len(got) != 1 {
		t.Fatalf("lists: %+v", got)
	}
	if got := run(t, "adguard.disabled", sources.DemoAdGuard(), todayEnv(nil)); len(got) != 0 {
		t.Fatalf("adguard enabled but reported: %+v", got)
	}
}

func TestNextcloudSabnzbdRules(t *testing.T) {
	nc := sources.DemoNextcloud()
	if got := run(t, "nextcloud.disk_low", nc, todayEnv(nil)); len(got) != 1 || got[0].Severity != enums.SeverityWarn || got[0].Params["gb"] != 8 {
		t.Fatalf("disk: %+v", got)
	}
	if got := run(t, "nextcloud.app_updates", nc, todayEnv(nil)); len(got) != 1 {
		t.Fatalf("apps: %+v", got)
	}

	sab := sources.DemoSabnzbd(time.Now())
	if got := run(t, "sabnzbd.failed", sab, todayEnv(nil)); len(got) != 1 {
		t.Fatalf("failed: %+v", got)
	}
	if got := run(t, "sabnzbd.disk_low", sab, todayEnv(nil)); len(got) != 1 || got[0].Severity != enums.SeverityWarn {
		t.Fatalf("disk: %+v", got)
	}
}

func TestGluetunRules(t *testing.T) {
	data := sources.DemoGluetun()
	if got := run(t, "gluetun.vpn", data, todayEnv(nil)); len(got) != 1 || got[0].Message != "gluetun.country" {
		t.Fatalf("country: %+v", got)
	}
	data.ExitIP = data.OwnIP
	data.Status = "stopped"
	if got := run(t, "gluetun.vpn", data, todayEnv(nil)); len(got) != 3 {
		t.Fatalf("leak and down: %+v", got)
	}
}

func TestDomainsBlacklistRules(t *testing.T) {
	// example.org in 18 days → warn; example.de has no date.
	if got := run(t, "domains.expiring", sources.DemoDomains(time.Now()), todayEnv(nil)); len(got) != 1 || got[0].Severity != enums.SeverityWarn {
		t.Fatalf("domains: %+v", got)
	}
	data := sources.DemoBlacklist()
	data.Refused = []string{"zen.spamhaus.org"}
	got := run(t, "blacklist.listed", data, todayEnv(nil))
	if len(got) != 2 || got[0].Params["zone"] != "bl.spamcop.net" || got[1].Message != "blacklist.refused" {
		t.Fatalf("blacklist: %+v", got)
	}
}

// A domain without SPF or DMARC can be used to send mail in its name;
// unchecked domains (DNS failed) stay quiet.
func TestDomainsMailAuth(t *testing.T) {
	data := &sources.DomainsDataset{Domains: []sources.DomainInfo{
		{Name: "ok.example", MailChecked: true, SPF: true, DMARC: true},
		{Name: "half.example", MailChecked: true, SPF: true},
		{Name: "bare.example", MailChecked: true},
		{Name: "unknown.example"},
	}}
	got := run(t, "domains.mail_auth", data, todayEnv(nil))
	if len(got) != 2 || got[0].Params["missing"] != "DMARC" || got[1].Params["missing"] != "SPF, DMARC" {
		t.Fatalf("findings: %+v", got)
	}
}
