package widgets

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// TestKimaiTimerFormKeepsOptions: quick starts and the note field can
// be set in the form and survive saving.
func TestKimaiTimerFormKeepsOptions(t *testing.T) {
	form := map[string]string{FormPrefix + FormMarker: "1", FormPrefix + "recent": "6", FormPrefix + "ask_note": "on"}
	cfg := decodeKimaiLite(ParseForm("kimai_timer", func(name string) string { return form[name] })).(KimaiLiteConfig)
	if cfg.Recent != 6 || !cfg.AskNote {
		t.Fatalf("cfg: %+v", cfg)
	}
}

// TestRedAboveHighWarn: a warning from 90 % still leaves room for red.
func TestRedAboveHighWarn(t *testing.T) {
	if got := meterTier(97, 90); got != "red" {
		t.Fatalf("sysinfo 97 %% at warn 90: %s", got)
	}

	data := &sources.TrueNASDataset{Pools: []sources.Pool{{Name: "tank", Status: "ONLINE", Healthy: true, Size: 100, Allocated: 97}}}
	view := truenasView(decodeTrueNAS(map[string]any{"warn_pct": 90.0}), map[string]any{"data": data}, ViewCtx{})
	if pools := view["Pools"].([]PoolBar); pools[0].Tier != "red" {
		t.Fatalf("pool 97 %% at warn 90: %q", pools[0].Tier)
	}
}

// TestKomodoCalmWithStoppedStack: a stack stopped on purpose is no
// reason to show the tile.
func TestKomodoCalmWithStoppedStack(t *testing.T) {
	data := &sources.KomodoDataset{Stacks: []sources.KStack{{Name: "web", State: "running"}, {Name: "old", State: "stopped"}}}
	ctx := ViewCtx{Options: map[string]any{"stopped": []any{"old"}}}
	view := komodoView(decodeKomodo(map[string]any{}), map[string]any{"data": data}, ctx)
	if !IsCalm("komodo_stacks", view) {
		t.Fatalf("not calm: %+v", view)
	}
}

// TestPaperlessCalmCountsTag: with a tag the tile counts that tag, so
// only an empty tag is calm.
func TestPaperlessCalmCountsTag(t *testing.T) {
	data := &sources.PaperlessDataset{Inbox: 0, TagCounts: map[string]int{"todo": 3}}
	view := paperlessInboxView(decodePaperless(map[string]any{"tag": "todo"}), map[string]any{"data": data}, ViewCtx{})
	if IsCalm("paperless_inbox", view) {
		t.Fatal("calm with 3 tagged documents")
	}
}

// TestHolidaysStateGetsCountry: "BY" means "DE-BY", as Nager names it.
func TestHolidaysStateGetsCountry(t *testing.T) {
	if cfg := decodeHolidays(map[string]any{"country": "de", "state": "by"}).(HolidaysConfig); cfg.State != "DE-BY" {
		t.Fatalf("state %q", cfg.State)
	}
	if cfg := decodeHolidays(map[string]any{"state": "DE-BY"}).(HolidaysConfig); cfg.State != "DE-BY" {
		t.Fatalf("state %q", cfg.State)
	}
}

// TestExpiryUnknownDateLast: a domain without a known end neither reads
// "-106751 days" nor sorts first.
func TestExpiryUnknownDateLast(t *testing.T) {
	now := time.Now()
	certs := &sources.CertDataset{Certs: []sources.Cert{{Host: "a", NotAfter: now.AddDate(0, 0, 20)}}}
	doms := &sources.DomainsDataset{Domains: []sources.DomainInfo{{Name: "b"}}}
	view := expiryView(decodeExpiry(map[string]any{}), map[string]any{"data": certs, peerDomains: doms},
		ViewCtx{Today: now.Format(time.DateOnly)})
	bars := view["Bars"].([]HBar)
	if len(bars) != 2 || bars[0].Label != "a" || bars[1].Value != expiryUnknown {
		t.Fatalf("bars: %+v", bars)
	}
}

// TestClockDropsUnknownZones: a typo would show "?" in the browser.
func TestClockDropsUnknownZones(t *testing.T) {
	cfg := decodeClock(map[string]any{"timezones": []any{"Europe/Berlin", "Europe/Berln"}}).(ClockConfig)
	if len(cfg.Timezones) != 1 || cfg.Timezones[0] != "Europe/Berlin" {
		t.Fatalf("zones: %v", cfg.Timezones)
	}
}

// TestGiteaHeadFollowsShow: showing only issues heads with their count.
func TestGiteaHeadFollowsShow(t *testing.T) {
	data := &sources.GiteaDataset{Reviews: []sources.Issue{{Title: "r"}}, Assigned: []sources.Issue{{Title: "a"}, {Title: "b"}}}
	view := giteaView(decodeListOf("show")(map[string]any{"show": "issues"}), map[string]any{"data": data}, ViewCtx{})
	if view["Head"] != 2 || view["HeadKey"] != "gitea.assigned" {
		t.Fatalf("head: %v %v", view["Head"], view["HeadKey"])
	}
}

// TestRateTrendNamesItsMonths: the caption says how many months the
// line covers.
func TestRateTrendNamesItsMonths(t *testing.T) {
	ninja := &sources.NinjaDataset{Currency: "EUR", Invoices: []sources.NinjaInvoice{{ID: 1, Status: "paid", Date: "2026-08-10", Net: 1000}}}
	kimai := &sources.KimaiDataset{Timesheets: []sources.KimaiSheet{{Begin: "2026-08-03", Minutes: 600}}}
	view := rateTrendView(decodeRateTrend(map[string]any{"months": 6.0}), map[string]any{"data": ninja, peerKimai: kimai},
		ViewCtx{Today: "2026-09-15"})
	if view["Months"] != 6 {
		t.Fatalf("months: %v", view["Months"])
	}
}

// TestHintsMinSeverityFromSelect: the select sends "20"; stored
// numbers keep working.
func TestHintsMinSeverityFromSelect(t *testing.T) {
	for _, raw := range []any{"20", 20.0} {
		if cfg := decodeHints(map[string]any{"min_severity": raw}).(HintsConfig); cfg.MinSeverity != 20 {
			t.Errorf("%#v: %d", raw, cfg.MinSeverity)
		}
	}
	for _, v := range FormValues("hints", map[string]any{"min_severity": 30.0}) {
		if v.Key == "min_severity" && (v.Input != InputSelect || v.Text != "30") {
			t.Errorf("field: %s %q", v.Input, v.Text)
		}
	}
}

// TestCheckConfig: a window or time zone the tile cannot read is
// refused on save instead of silently meaning "always" or UTC.
func TestCheckConfig(t *testing.T) {
	cases := []struct {
		key  string
		raw  map[string]any
		want string
	}{
		{"update_window", map[string]any{"window": "22:00-06:00", "timezone": "Europe/Berlin"}, ""},
		{"update_window", map[string]any{}, ""},
		{"update_window", map[string]any{"window": "25-3"}, CheckBadWindow},
		{"update_window", map[string]any{"window": "22"}, CheckBadWindow},
		{"update_window", map[string]any{"timezone": "Europe/Berln"}, CheckBadTimezone},
		{"today", map[string]any{"timezone": "Mars/Base"}, CheckBadTimezone},
		{"greeting", map[string]any{"timezone": "Mars/Base"}, CheckBadTimezone},
		{"note", map[string]any{"timezone": "Mars/Base"}, ""},
	}
	for _, c := range cases {
		if got := Check(c.key, c.raw); got != c.want {
			t.Errorf("%s %v: %q, want %q", c.key, c.raw, got, c.want)
		}
	}
}
