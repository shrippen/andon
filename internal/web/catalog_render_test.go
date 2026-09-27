package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/services/widgetlib"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// Every catalog tile renders from its service's demo dataset.
func TestCatalogTilesRender(t *testing.T) {
	now := time.Now()
	today := now.Format(time.DateOnly)
	speed := &metrics.History{Series: map[string][]metrics.Point{}}
	for i := range 7 {
		speed.Series[metrics.SampleKey("speedtest", "down")] = append(speed.Series[metrics.SampleKey("speedtest", "down")],
			metrics.Point{Day: now.AddDate(0, 0, i-6), Value: float64(150 + i*15)})
	}

	cases := map[string]struct {
		data  any
		want  string
		peers map[string]any // other datasets of the space, by query name
	}{
		"kimai_week":       {data: sources.DemoKimai(now), want: "weekcol"},
		"kimai_split":      {data: sources.DemoKimai(now), want: "weekcol"},
		"unbilled_age":     {data: sources.DemoKimai(now), want: "hbar"},
		"disks":            {data: sources.DemoScrutiny(now), want: "legend-list"},
		"komodo_stacks":    {data: sources.DemoKomodo(now), want: "strip"},
		"truenas_pools":    {data: sources.DemoTrueNAS(), want: "hbar"},
		"pihole":           {data: sources.DemoPihole(now), want: "progress-bar"},
		"adguard":          {data: sources.DemoAdGuard(), want: "progress-bar"},
		"vpn":              {data: sources.DemoGluetun(), want: "pill"},
		"gateway":          {data: sources.DemoGateway(), want: "WAN_DHCP"},
		"expiry":           {data: sources.DemoCerts(now), want: "hbar"},
		"speed_history":    {data: &sources.SpeedtestDataset{Down: 240, Up: 40, ExpectDown: 250, At: now}, want: "bars-target"},
		"sabnzbd":          {data: sources.DemoSabnzbd(now), want: "MB/s"},
		"paperless_inbox":  {data: sources.DemoPaperless(now), want: "Posteingang"},
		"mail_invoices":    {data: sources.DemoMail(now), want: "tile-value"},
		"freshrss_feeds":   {data: sources.DemoFreshRSS(now), want: "hbar"},
		"linkwarden":       {data: sources.DemoLinkwarden(), want: "hbar"},
		"kintsugi":         {data: sources.DemoKintsugi(now), want: "Stadtwerke"},
		"gitea_reviews":    {data: sources.DemoGitea(now), want: "Reviews offen"},
		"dawarich_day":     {data: sources.DemoDawarich(now), want: "kl-day"},
		"authentik_logins": {data: sources.DemoAuthentik(now), want: "Anmeldungen"},
		"vaultwarden_2fa":  {data: sources.DemoVaultwarden(now), want: "progress-bar"},
		"monitors":         {data: sources.DemoKuma(), want: "strip-lg"},
		"payment_days":     {data: sources.DemoNinja(now), want: "pay-scale"},
		"month_close":      {want: "close-steps", peers: map[string]any{"kimai": sources.DemoKimai(now), "invoiceninja": sources.DemoNinja(now)}},
		"today":            {want: "day-line", peers: map[string]any{"calendar": sources.DemoCalendar(now), "kimai": sources.DemoKimai(now)}},
		"receipts_missing": {data: sources.DemoSure(now), want: "slot-note"},
		"travel":           {data: sources.DemoDawarich(now), want: "412 km"},
		"exposure":         {data: sources.DemoPangolin(), want: "expo-rows"},
		"subscriptions":    {want: "Hetzner", peers: map[string]any{"wallos": sources.DemoWallos(now), "sure": sources.DemoSure(now)}},
		"rate_trend":       {data: sources.DemoNinja(now), want: "spark", peers: map[string]any{"kimai": sources.DemoKimai(now)}},
	}
	for key, c := range cases {
		kind, ok := widgets.Get(key)
		if !ok {
			t.Fatalf("%s not registered", key)
		}
		cfg, _ := widgets.Decode(key, map[string]any{})
		results := map[string]any{"data": c.data}
		for name, peer := range c.peers {
			results[name] = peer
		}
		if key == "speed_history" {
			results[widgets.HistorySlot] = speed
		}
		view := kind.View(cfg, results, widgets.ViewCtx{Today: today})
		frag := &widgetlib.Fragment{Type: key, View: view, Slots: map[string]widgetlib.Slot{"data": {Data: c.data}}}

		rec := httptest.NewRecorder()
		if err := (Deps{}).Page(rec, Ctx{Locale: enums.LocaleDE}, kind.Template, http.StatusOK, map[string]any{"ThemeURL": "", "Frag": frag}); err != nil {
			t.Fatalf("%s: %v", key, err)
		}
		if body := rec.Body.String(); !strings.Contains(body, c.want) {
			t.Errorf("%s: missing %q\n%s", key, c.want, body)
		}
	}
}

// TestGreetingForecastShowsWeekdays: the four forecast columns are labelled
// by weekday ("Sa"), the full date only as a tooltip, so narrow tiles do
// not overlap the labels.
func TestGreetingForecastShowsWeekdays(t *testing.T) {
	view := map[string]any{"HasWeather": true, "Temp": 17.0, "Code": 3, "Wind": 1.0, "Timezone": "Europe/Berlin",
		"Days": []widgets.ForecastDay{{Day: "2026-09-26", Max: 24, Min: 3, Height: 90}, {Day: "2026-09-27", Max: 20, Min: 5, Height: 70}}}
	frag := &widgetlib.Fragment{Type: "greeting", View: view, Slots: map[string]widgetlib.Slot{"weather": {Data: &sources.WeatherResult{}}}}

	rec := httptest.NewRecorder()
	if err := (Deps{}).Page(rec, Ctx{Locale: enums.LocaleDE}, "widgets/greeting", http.StatusOK, map[string]any{"ThemeURL": "", "Frag": frag}); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `title="26.09.2026">Sa</small>`) || !strings.Contains(body, `>So</small>`) {
		t.Fatalf("forecast labels:\n%s", body)
	}
}

// TestGatewayTileOpenWrt: an OpenWrt router shows its model and clients,
// and no loss, delay or update count it cannot know.
func TestGatewayTileOpenWrt(t *testing.T) {
	kind, _ := widgets.Get("gateway")
	data := &sources.GatewayDataset{Kind: "openwrt", Version: "24.10.2", Model: "GL-MT6000", Clients: 14,
		Gateways: []sources.GatewayLink{{Name: "wan", Up: true}}}
	view := kind.View(nil, map[string]any{"data": data}, widgets.ViewCtx{})
	frag := &widgetlib.Fragment{Type: "gateway", View: view, Slots: map[string]widgetlib.Slot{"data": {Data: data}}}
	rec := httptest.NewRecorder()
	if err := (Deps{}).Page(rec, Ctx{Locale: enums.LocaleDE}, "widgets/gateway", http.StatusOK, map[string]any{"ThemeURL": "", "Frag": frag}); err != nil {
		t.Fatal(err)
	}
	body := rec.Body.String()
	for _, want := range []string{"GL-MT6000", ">14<", "Geräte im Netz"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "Verlust") || strings.Contains(body, "Updates") {
		t.Fatalf("shows what OpenWrt does not report:\n%s", body)
	}
}
