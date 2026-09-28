package sources

import (
	"fmt"
	"math/rand"
	"strings"
	"time"

	"andon/internal/sources/demoworld"
)

// Generated demo datasets for demo:// connections, relative to today and
// deterministic. Names, places and receipts come from Studio Weber, the demo
// world shared by all shrippen projects (package demoworld). The datasets fit
// together so every rule fires once:
//
//	Northlight Pictures  visited on demoSkipDay without a Kimai entry, unbilled hours, overdue invoice
//	Speiche              budget at 85 %, quote without reaction
//	Donaulicht Film      EU client (Austria), invoice at 0 % without VAT id

const (
	demoSeed         = 7
	demoRate         = 95.0
	demoVAT          = 0.19
	demoSkipDay      = 3
	demoUnbilledDays = 40
	demoHistoryDays  = 600
	demoClientWindow = 25
	demoRunningHours = 11
	demoInvoiceMonth = 20
	demoHomeCountry  = "276"
	demoEUCountry    = "40"
	demoGearReceipt  = 2 // a studio supplier's receipt, its vendor paid from the business account
)

var (
	demoWorld      = demoworld.Get()
	demoClientDays = []int{1, 3, 8, 10, 15}
	// Customer ids 1-3 in every demo dataset: on-site client, budget client, EU client.
	demoCustomerIDs = []string{"northlight", "speiche", "donaulicht"}
	demoProjectIDs  = []string{"harbour", "spring", "trailer"}
	demoHome        = demoWorld.Place("home-mara")
	demoSite        = demoWorld.Place("northlight-office")
	demoCustomers   = demoKimaiCustomers()
	demoEdit        = demoWorld.Activity("edit").Name.DE()
	demoOnSite      = demoWorld.Activity("shoot").Name.DE() + " vor Ort"
	demoMeeting     = demoWorld.Activity("meeting").Name.DE()
	// Suppliers: the mobile contract in Paperless, the hosting that Sure,
	// Wallos and the mailbox know, the software subscription missing in Wallos.
	demoMobile   = demoWorld.Vendor("elbnetz")
	demoHosting  = demoWorld.Vendor("nordhost")
	demoSoftware = demoWorld.Vendor("farbraum")
	demoDisks    = demoWorld.Inventory.Disks
)

func demoKimaiCustomers() []KimaiCustomer {
	out := make([]KimaiCustomer, len(demoCustomerIDs))
	for i, id := range demoCustomerIDs {
		out[i] = KimaiCustomer{int64(i + 1), demoWorld.Customer(id).Name}
	}
	return out
}

func demoProjectName(i int) string { return demoWorld.Project(demoProjectIDs[i]).Name.DE() }

// demoSiteName is the Dawarich area of the on-site client.
func demoSiteName() string { return demoSite.Name.DE() }

func demoCountry(code string) string {
	if code == "AT" {
		return demoEUCountry
	}
	return demoHomeCountry
}

func demoDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// demoMonday is Monday of the week of today, the anchor of the demo world's days.
func demoMonday(today time.Time) time.Time {
	return today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
}

func iso(t time.Time) string { return t.Format(time.DateOnly) }

func stamp(d time.Time, hour, minute int) string {
	return time.Date(d.Year(), d.Month(), d.Day(), hour, minute, 0, 0, time.UTC).Format(time.RFC3339)
}

// weekdaysBack lists the weekdays before today, newest first.
func weekdaysBack(today time.Time, days int) []time.Time {
	var found []time.Time
	for back := 1; back <= days; back++ {
		d := today.AddDate(0, 0, -back)
		if d.Weekday() != time.Saturday && d.Weekday() != time.Sunday {
			found = append(found, d)
		}
	}
	return found
}

func clientDays(today time.Time) []time.Time {
	days := weekdaysBack(today, demoClientWindow)
	limit := demoClientDays[len(demoClientDays)-1] + 1
	if len(days) > limit {
		days = days[:limit]
	}
	return days
}

// DemoKimai is the demo Kimai dataset.
func DemoKimai(now time.Time) *KimaiDataset {
	today := demoDay(now)
	rnd := rand.New(rand.NewSource(demoSeed))
	clients := clientDays(today)
	visits := map[time.Time]bool{}
	for _, i := range demoClientDays {
		if i < len(clients) {
			visits[clients[i]] = true
		}
	}
	skip := clients[demoSkipDay]

	history := weekdaysBack(today, demoHistoryDays)
	sheets := make([]KimaiSheet, 0, len(history))
	for i := len(history) - 1; i >= 0; i-- {
		d := history[i]
		customer := []int64{1, 2, 2, 3}[rnd.Intn(4)]
		if visits[d] {
			customer = 1
		}
		if d.Equal(skip) {
			customer = 2
		}
		hours := 5 + rnd.Intn(4)
		age := int(today.Sub(d).Hours() / 24)
		activity := demoEdit
		if visits[d] {
			activity = demoOnSite
		}
		sheets = append(sheets, KimaiSheet{
			ID: int64(len(sheets) + 1), Begin: stamp(d, 9, 0), End: stamp(d, 9+hours, 0),
			Minutes: hours * 60, Rate: float64(hours) * demoRate, Billable: true,
			Exported:  !(customer == 1 && age <= demoUnbilledDays) && age > 5,
			ProjectID: customer, CustomerID: customer, Activity: activity, UserID: 1,
		})
	}

	running := now.UTC().Add(-demoRunningHours * time.Hour)
	return &KimaiDataset{
		URL:        "https://kimai.demo",
		Contract:   DemoContract(),
		Timesheets: sheets,
		Active: []KimaiSheet{{ID: 9999, Begin: running.Format(time.RFC3339), Billable: true,
			ProjectID: 2, CustomerID: 2, Activity: demoEdit, UserID: 1}},
		Projects: []KimaiProject{
			{ID: 1, Name: demoProjectName(0), CustomerID: 1},
			{ID: 2, Name: demoProjectName(1), CustomerID: 2, Budget: 40000, End: iso(today.AddDate(0, 0, 60)), UsedMoney: 34000},
			{ID: 3, Name: demoProjectName(2), CustomerID: 3, TimeBudgetMin: 40 * 60, BudgetType: "month"},
		},
		Customers: demoCustomers,
		Absences: []KimaiAbsence{{Start: iso(today.AddDate(0, 0, 20)), End: iso(today.AddDate(0, 0, 24)),
			Type: "holiday", Status: "approved"}},
		Holidays:      []KimaiHoliday{{Date: time.Date(today.Year(), time.December, 25, 0, 0, 0, 0, time.UTC).Format(time.DateOnly), Name: "1. Weihnachtstag"}},
		HolidayBundle: true,
	}
}

// DemoNinja is the demo Invoice Ninja dataset.
func DemoNinja(now time.Time) *NinjaDataset {
	today := demoDay(now)
	rnd := rand.New(rand.NewSource(demoSeed))
	var invoices []NinjaInvoice
	var payments []NinjaPayment
	number := int64(1)

	for back := demoInvoiceMonth; back > 0; back-- {
		start := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -back, 0)
		for _, c := range demoCustomers {
			net := round2(2500 + rnd.Float64()*4000)
			tax := round2(net * demoVAT)
			if c.ID == 3 && back == 1 {
				tax = 0
			}
			issued := start.AddDate(0, 0, 2)
			invoices = append(invoices, NinjaInvoice{
				ID: number, Number: "R-" + issued.Format("2006") + "-" + pad3(number), ClientID: c.ID, Status: "paid",
				Date: iso(issued), DueDate: iso(issued.AddDate(0, 0, 14)), Amount: net + tax, Taxes: tax, Net: net,
			})
			payments = append(payments, NinjaPayment{ID: number, Date: iso(issued.AddDate(0, 0, 10)), Amount: net + tax, ClientID: c.ID})
			number++
		}
	}

	overdue := &invoices[len(invoices)-3]
	overdue.Status, overdue.Balance, overdue.DueDate = "sent", overdue.Amount, iso(today.AddDate(0, 0, -21))
	kept := payments[:0]
	for _, p := range payments {
		if p.ID != overdue.ID {
			kept = append(kept, p)
		}
	}
	invoices = append(invoices, NinjaInvoice{ID: number, ClientID: 2, Status: "draft", Date: iso(today.AddDate(0, 0, -10)),
		Amount: 1190, Balance: 1190, Taxes: 190, Net: 1000})

	return &NinjaDataset{
		URL: "https://invoice.demo", Currency: "EUR", Invoices: invoices, Payments: kept,
		Clients:  demoNinjaClients(),
		Expenses: demoNinjaExpenses(today),
		Quotes: []NinjaQuote{{ID: 1, Number: "A-" + today.Format("2006") + "-004", ClientID: 2, Status: "sent",
			Date: iso(today.AddDate(0, 0, -20)), Amount: 8330}},
		Recurring: []NinjaRecurring{{ID: 1, Number: "W-01", ClientID: 1, Active: true,
			NextSendDate: iso(today.AddDate(0, 0, 12)), RemainingCycles: 1, Amount: 595}},
		HomeCountryID: demoHomeCountry,
	}
}

func demoNinjaClients() []NinjaClient {
	out := make([]NinjaClient, len(demoCustomerIDs))
	for i, id := range demoCustomerIDs {
		c := demoWorld.Customer(id)
		out[i] = NinjaClient{ID: int64(i + 1), Name: c.Name, VATNumber: c.VATID, CountryID: demoCountry(c.Country)}
	}
	return out
}

// demoNinjaExpenses are the receipts of the demo world. Their days are
// offsets from Monday of the current week.
func demoNinjaExpenses(today time.Time) []NinjaExpense {
	monday := demoMonday(today)
	out := make([]NinjaExpense, 0, len(demoWorld.Receipts))
	for _, r := range demoWorld.Receipts {
		out = append(out, NinjaExpense{ID: int64(r.ID), Date: iso(monday.AddDate(0, 0, r.Day)), Amount: r.Amount,
			Tax: round2(r.Amount * demoVAT / (1 + demoVAT)), Notes: r.Vendor + " · " + r.Note.DE(), VendorID: int64(r.ID)})
	}
	return out
}

func pad3(n int64) string {
	s := []byte{'0', '0', '0'}
	for i := 2; i >= 0 && n > 0; i-- {
		s[i] = byte('0' + n%10)
		n /= 10
	}
	return string(s)
}

// demoSnipeAssets gives the studio's assets from the demo world their
// states: the notebook's warranty ends soon, the NAS is past end of life
// and overdue for an audit, the fourth asset should have come back.
func demoSnipeAssets(ago, ahead func(int) string) []SnipeAsset {
	states := []SnipeAsset{
		{Status: "Ausgegeben", Deployable: true, Assigned: true, PurchaseDate: ago(30), WarrantyExpires: ahead(10),
			EOLDate: ahead(900), NextAudit: ahead(100), LastChange: ago(30)},
		{Status: "In Betrieb", Deployable: true, Assigned: true, PurchaseDate: ago(1900), WarrantyExpires: ago(800),
			EOLDate: ago(20), NextAudit: ago(15), LastChange: ago(400)},
		{Status: "Bereit", Deployable: true, PurchaseDate: ago(500), WarrantyExpires: ahead(230),
			NextAudit: ahead(60), LastChange: ago(140)},
		{Status: "Ausgegeben", Deployable: true, Assigned: true, PurchaseDate: ago(60), WarrantyExpires: ahead(1000),
			NextAudit: ahead(200), LastChange: ago(60), AssignedTo: demoWorld.Person("jonas").Name, ExpectedCheckin: ago(5)},
	}
	out := make([]SnipeAsset, len(states))
	for i, a := range demoWorld.Inventory.Assets[:len(states)] {
		s := states[i]
		s.ID, s.Name, s.Tag, s.Model, s.Category, s.PurchaseCost = int64(i+1), a.Name, a.Tag, a.Model, a.Category.DE(), a.Cost
		out[i] = s
	}
	return out
}

// demoSnipeLicenses: the first licence expires soon, the second has free seats.
func demoSnipeLicenses(ahead func(int) string) []SnipeLicense {
	l := demoWorld.Inventory.Licenses
	return []SnipeLicense{
		{ID: 1, Name: l[0].Name, Expires: ahead(20), Seats: l[0].Seats},
		{ID: 2, Name: l[1].Name, Expires: ahead(200), Seats: l[1].Seats, Free: 2},
	}
}

// DemoSnipe is the demo Snipe-IT dataset.
func DemoSnipe(now time.Time) *SnipeDataset {
	today := demoDay(now)
	ago := func(n int) string { return iso(today.AddDate(0, 0, -n)) }
	ahead := func(n int) string { return iso(today.AddDate(0, 0, n)) }
	return &SnipeDataset{
		URL:          "https://assets.demo",
		Assets:       demoSnipeAssets(ago, ahead),
		Licenses:     demoSnipeLicenses(ahead),
		Consumables:  []SnipeConsumable{{ID: 1, Name: "Toner schwarz", Remaining: 1, Min: 2}},
		AuditOverdue: []int64{2},
	}
}

// DemoDawarich is the demo Dawarich dataset.
func DemoDawarich(now time.Time) *DawarichDataset {
	today := demoDay(now)
	days := clientDays(today)
	lat, lon := demoSite.Lat, demoSite.Lon
	var visits []DawarichVisit
	for _, i := range demoClientDays {
		if i >= len(days) {
			continue
		}
		visits = append(visits, DawarichVisit{ID: int64(i), Start: stamp(days[i], 8, 30), End: stamp(days[i], 17, 45),
			Minutes: 555, AreaID: 2, Name: demoSiteName(), Lat: &lat, Lon: &lon})
	}
	return &DawarichDataset{
		URL: "https://dawarich.demo",
		Areas: []DawarichArea{
			{ID: 1, Name: demoHome.Name.DE(), Lat: demoHome.Lat, Lon: demoHome.Lon, Radius: 100},
			{ID: 2, Name: demoSiteName(), Lat: lat, Lon: lon, Radius: 150},
		},
		Visits: visits,
		Stats: map[string]any{"totalDistanceKm": 18450.0, "yearlyStats": []any{map[string]any{
			"year": float64(now.Year()), "totalDistanceKm": 3100.0, "totalCountriesVisited": 2.0, "totalCitiesVisited": 14.0,
			"monthlyDistanceKm": map[string]any{
				strings.ToLower(now.Month().String()):                   412.0,
				strings.ToLower(now.AddDate(0, -1, 0).Month().String()): 530.0,
			},
		}}},
		LastPoint: now.UTC().Add(-2 * time.Hour).Format(time.RFC3339),
	}
}

// DemoKuma is the demo Uptime Kuma dataset.
func DemoKuma() *KumaDataset {
	return &KumaDataset{URL: "https://status.demo", Monitors: []KumaMonitor{
		{Name: "Kimai", Type: "http", Target: "https://zeit.demo", Status: KumaUp, CertDays: 54, MS: 180},
		{Name: "NAS", Type: "ping", Status: KumaDown, CertDays: -1},
		{Name: "Shop", Type: "http", Target: "https://shop.demo", Status: KumaUp, CertDays: 9, MS: 420},
	}}
}

// DemoGlances is the demo Glances host: busy CPU, one disk filling up.
func DemoGlances() *GlancesResult {
	return &GlancesResult{URL: "https://glances.demo", CPU: 38, Mem: 64, Swap: 4, Load: 1.2, Cores: 4,
		Disks: []GlancesDisk{{Mount: "/", Percent: 52}, {Mount: "/data", Percent: 87}}}
}

// DemoGlancesHistory is one demo metric over the last hour, a sample a minute.
func DemoGlancesHistory(now time.Time, metric string, points int) *GlancesHistory {
	rnd := rand.New(rand.NewSource(demoSeed))
	out := &GlancesHistory{Metric: metric}
	for i := points; i > 0; i-- {
		at := now.UTC().Add(-time.Duration(i) * time.Minute).Format("2006-01-02T15:04:05")
		out.Samples = append(out.Samples, Sample{At: at, Value: 20 + rnd.Float64()*40})
	}
	return out
}

// DemoProxmox is the demo Proxmox VE dataset.
func DemoProxmox(now time.Time) *ProxmoxDataset {
	today := demoDay(now)
	return &ProxmoxDataset{
		URL: "https://pve.demo:8006",
		Nodes: []ProxmoxNode{{Name: "pve", Online: true, Updates: 12, Storages: []ProxmoxStorage{
			{Name: "local-lvm", Used: 430e9, Total: 480e9}, {Name: "backup", Used: 1.1e12, Total: 4e12},
		}}},
		Guests: []ProxmoxGuest{
			{VMID: 100, Name: "docker", Node: "pve", Running: true, CPU: 1.2, MemBytes: 6e9},
			{VMID: 101, Name: "homeassistant", Node: "pve", Running: true, CPU: 0.3, MemBytes: 2e9},
			{VMID: 9000, Name: "debian-template", Node: "pve", Template: true},
		},
		Backups: map[int64]time.Time{100: today.AddDate(0, 0, -1), 101: today.AddDate(0, 0, -9)},
	}
}

// DemoPaperless is the demo Paperless-ngx dataset.
func DemoPaperless(now time.Time) *PaperlessDataset {
	today := demoDay(now)
	return &PaperlessDataset{URL: "https://docs.demo", Total: 1843, Inbox: 7, OldestTitle: "Rechnung " + demoMobile.Name,
		OldestAdded: iso(today.AddDate(0, 0, -23)),
		Invoices: []PaperlessDoc{{ID: 311, Title: "Rechnung 09/2026", Correspondent: demoMobile.Name,
			Created: iso(today.AddDate(0, 0, -23)), Amount: demoMobile.Monthly}},
		Contracts: []PaperlessContract{{ID: 88, Title: demoMobile.Contract.DE(), Correspondent: demoMobile.Name,
			End: today.AddDate(0, 3, 20), NoticeMonths: 3, Deadline: today.AddDate(0, 0, 20), RenewsAutomatic: true}},
		Newest: []PaperlessNew{{ID: 1843, Title: "Kontoauszug 09/2026", Added: iso(today)}, {ID: 1842, Title: "Rechnung " + demoHosting.Name, Added: iso(today.AddDate(0, 0, -1))},
			{ID: 1841, Title: "Versicherungsschein", Added: iso(today.AddDate(0, 0, -2))}},
		TagCounts: map[string]int{"steuer 2026": 64, "belege": 212}}
}

// DemoCerts is the demo certificate dataset.
func DemoCerts(now time.Time) *CertDataset {
	today := demoDay(now)
	return &CertDataset{Certs: []Cert{
		{Host: "shop.demo:443", NotAfter: today.AddDate(0, 0, 9), Issuer: "R11"},
		{Host: "zeit.demo:443", NotAfter: today.AddDate(0, 0, 54), Issuer: "R10"},
	}}
}

// DemoScrutiny is the demo Scrutiny dataset.
func DemoScrutiny(now time.Time) *ScrutinyDataset {
	seen := now.UTC().Add(-3 * time.Hour)
	return &ScrutinyDataset{URL: "https://disks.demo", Disks: []Disk{
		{Name: "sda", Model: demoDisks[0], Status: ScrutinyPassed, Temp: 38, Hours: 31000, Seen: seen},
		{Name: "sdb", Model: demoDisks[1], Status: 1, Temp: 41, Hours: 42000, Seen: seen},
		{Name: "nvme0", Model: demoDisks[2], Status: ScrutinyPassed, Temp: 56, Hours: 9000, Seen: seen},
	}}
}

// DemoImmich is the demo Immich dataset.
func DemoImmich() *ImmichDataset {
	return &ImmichDataset{URL: "https://photos.demo", Photos: 48213, Videos: 1920, DiskPercent: 87.4,
		DiskAvailable: "412 GiB", FailedJobs: map[string]int{"faceDetection": 3}, Version: "v1.131.0", Latest: "v1.132.3"}
}

// DemoUmami is the demo Umami dataset.
func DemoUmami() *UmamiDataset {
	return &UmamiDataset{URL: "https://stats.demo", Sites: []Site{
		{ID: "1", Name: "Blog", Domain: "blog.demo", Views: 1840, Visitors: 610, PrevViews: 1720, PrevVisit: 590},
		{ID: "2", Name: "Shop", Domain: "shop.demo", Views: 120, Visitors: 41, PrevViews: 980, PrevVisit: 305},
	}}
}

// DemoFreshRSS is the demo FreshRSS dataset.
func DemoFreshRSS(now time.Time) *FreshRSSDataset {
	day := func(n int) time.Time { return now.UTC().AddDate(0, 0, -n) }
	return &FreshRSSDataset{URL: "https://rss.demo", Unread: 812, Feeds: []Feed{
		{ID: "feed/1", Title: "heise online", Category: "News", Unread: 540, Newest: day(0)},
		{ID: "feed/2", Title: "Go Blog", Category: "Tech", Unread: 12, Newest: day(9)},
		{ID: "feed/3", Title: "Altes Projektblog", Category: "Tech", Unread: 0, Newest: day(400)},
		{ID: "feed/4", Title: "Selfhosted Weekly", Category: "Tech", Unread: 260, Newest: day(2)},
	}}
}

// DemoGitea is the demo Gitea dataset.
func DemoGitea(now time.Time) *GiteaDataset {
	day := func(n int) time.Time { return now.UTC().AddDate(0, 0, n) }
	return &GiteaDataset{URL: "https://git.demo", User: "mara", Notifications: 4,
		Assigned: []Issue{
			{Repo: "studio/showreel", Title: "Neue Harbour-Lights-Szenen", URL: "https://git.demo/studio/showreel/issues/12", Number: 12, Due: day(-2), Updated: day(-10)},
			{Repo: "studio/website", Title: "Neues Theme", URL: "https://git.demo/studio/website/pulls/4", Number: 4, Pull: true, Updated: day(-21)},
		},
		Reviews: []Issue{{Repo: "team/infra", Title: "Traefik 3 Migration", URL: "https://git.demo/team/infra/pulls/7", Number: 7, Pull: true, Updated: day(-4)}},
		Repos: []Repo{
			{Name: "studio/showreel", URL: "https://git.demo/studio/showreel", Updated: day(0), FailedWorkflow: "test"},
			{Name: "mirror/linux", URL: "https://git.demo/mirror/linux", Mirror: true, MirrorUpdated: day(-12), Updated: day(-12)},
		},
	}
}

// DemoBorg is the demo Borg Backup Server dataset.
func DemoBorg(now time.Time) *BorgDataset {
	return &BorgDataset{URL: "https://borg.demo", Failed24h: 1, Completed24h: 5, UsedBytes: 3.1e12, TotalBytes: 4e12,
		LastBackup: now.UTC().Add(-7 * time.Hour), AgentsOutdated: 1,
		Clients: []BorgClient{
			{Name: "nas", Status: "online", LastSeen: now.UTC().Add(-time.Minute), LastBackup: now.UTC().Add(-7 * time.Hour)},
			{Name: "laptop", Status: "offline", LastSeen: now.UTC().AddDate(0, 0, -6), LastBackup: now.UTC().AddDate(0, 0, -6)},
		}}
}

// DemoHass is the demo Home Assistant dataset.
func DemoHass(now time.Time) *HassDataset {
	ago := func(h int) time.Time { return now.UTC().Add(-time.Duration(h) * time.Hour) }
	return &HassDataset{URL: "https://home.demo", Entities: []Entity{
		{ID: "binary_sensor.keller_wasser", Name: "Keller Wasser", Domain: "binary_sensor", State: "off", DeviceClass: "moisture", Changed: ago(200)},
		{ID: "light.buero", Name: "Büro Licht", Domain: "light", State: "on", Changed: ago(1)},
		{ID: "sensor.fenster_bad_batterie", Name: "Fenster Bad Batterie", Domain: "sensor", State: "8", Unit: "%", DeviceClass: "battery", Changed: ago(5)},
		{ID: "sensor.wohnzimmer_temperatur", Name: "Wohnzimmer", Domain: "sensor", State: "21.4", Unit: "°C", DeviceClass: "temperature", Changed: ago(0)},
		{ID: "sensor.zigbee_steckdose_power", Name: "Steckdose Leistung", Domain: "sensor", State: HassUnavailable, Unit: "W", Changed: ago(30)},
		{ID: "switch.kaffeemaschine", Name: "Kaffeemaschine", Domain: "switch", State: "off", Changed: ago(3)},
		{ID: "update.home_assistant_core_update", Name: "Home Assistant Core", Domain: "update", State: HassOn, Changed: ago(20)},
	}}
}

// DemoSure is the demo Sure dataset; one income matches the open demo
// invoice, one expected payment is overdue.
func DemoSure(now time.Time) *SureDataset {
	today := demoDay(now)
	day := func(n int) string { return iso(today.AddDate(0, 0, n)) }
	return &SureDataset{URL: "https://money.demo", Currency: "EUR", NetWorth: 48210,
		Accounts: []SureAccount{
			{ID: "a1", Name: "Geschäftskonto", Type: "depository", Classification: "asset", Balance: 6120, Currency: "EUR"},
			{ID: "a2", Name: "Tagesgeld", Type: "depository", Classification: "asset", Balance: 150, Currency: "EUR"},
		},
		Transactions: []SureTxn{
			{ID: "t1", Date: day(-3), Name: fmt.Sprintf("%s RE-2026-017", demoCustomers[0].Name), Amount: 2380, Category: "Einnahmen", Account: "Geschäftskonto"},
			{ID: "t2", Date: day(-5), Name: demoHosting.Name, Amount: -demoHosting.Monthly, Category: demoHosting.Kind.DE(), Merchant: demoHosting.Name, Account: "Geschäftskonto"},
			{ID: "t3", Date: day(-8), Name: demoWorld.Receipt(demoGearReceipt).Vendor, Amount: -899, Account: "Geschäftskonto"},
			{ID: "t4", Date: day(-12), Name: "Bäckerei", Amount: -6.4, Account: "Tagesgeld"},
			{ID: "t5", Date: day(-40), Name: demoHosting.Name, Amount: -demoHosting.Monthly, Category: demoHosting.Kind.DE(), Merchant: demoHosting.Name, Account: "Geschäftskonto"},
		},
		Recurring: []SureRecurring{
			{Name: demoHosting.Name, Status: "active", Amount: demoHosting.Monthly, Expense: true, Next: day(25), Last: day(-5)},
			{Name: "Krankenversicherung", Status: "active", Amount: 612, Expense: true, Next: day(-9), Last: day(-39)},
			{Name: "Miete Studio", Status: "active", Amount: 450, Expense: true, Next: day(6), Last: day(-24)},
			{Name: demoSoftware.Contract.DE(), Status: "active", Amount: demoSoftware.Monthly, Expense: true, Next: day(11), Last: day(-19)},
		},
	}
}

// DemoLinkwarden is the demo Linkwarden dataset: two bookmarks match
// Homelab tiles, one is missing there.
func DemoLinkwarden() *LinkwardenDataset {
	now := time.Now().UTC()
	return &LinkwardenDataset{URL: "https://links.demo", Collections: []string{"Homelab"}, Links: []Bookmark{
		{Name: "Kimai", URL: "https://www.kimai.org/", Collection: "Homelab", Created: now.AddDate(0, 0, -30)},
		{Name: "Invoice Ninja", URL: "https://invoiceninja.com", Collection: "Homelab", Created: now.AddDate(0, 0, -3)},
		{Name: "Grafana", URL: "https://grafana.com", Collection: "Homelab", Created: now.AddDate(0, 0, -1)},
	}}
}

// DemoKintsugi is the demo Kintsugi dataset: three open suggestions, the
// oldest a week old, and a failed last run.
func DemoKintsugi(now time.Time) *KintsugiDataset {
	ago := func(d int) time.Time { return now.AddDate(0, 0, -d) }
	base := "https://kintsugi.demo"
	return &KintsugiDataset{URL: base, New: 3, Accepted: 2, Snoozed: 1, Done: 4, Rejected: 2, Rate: 75, GapsOpen: 2,
		Open: []KintsugiSuggestion{
			{ID: 42, Kind: KintsugiAcquisition, Title: "Imagefilm für Stadtwerke anbieten", URL: base + "/vorschlaege#s-42", Created: ago(0)},
			{ID: 41, Kind: KintsugiDevelopment, Title: "Drohnenführerschein A2 machen", URL: base + "/vorschlaege#s-41", Created: ago(2)},
			{ID: 37, Kind: KintsugiAcquisition, Title: "Agentur Nordlicht nachfassen", URL: base + "/vorschlaege#s-37", Created: ago(8)},
		},
		LastRun:  &KintsugiRun{Status: KintsugiRunFailed, Detail: "LLM nicht erreichbar", At: ago(0)},
		Research: true, BudgetUSD: 5, UsedUSD: 1.25,
	}
}

// DemoPGBack is the demo PG Back Web dataset.
func DemoPGBack(now time.Time) *PGBackDataset {
	ago := func(h int) time.Time { return now.UTC().Add(-time.Duration(h) * time.Hour) }
	return &PGBackDataset{URL: "https://pgback.demo", LastEvent: ago(2), Backups: []PGBackup{
		{Name: "kimai", LastSuccess: ago(2)},
		{Name: "invoiceninja", LastSuccess: ago(50), LastFailure: ago(26)},
		{Name: "immich", LastSuccess: ago(80)},
	}}
}

// DemoMail is the demo mailbox: one invoice matches a demo expense
// amount, one does not.
func DemoMail(now time.Time) *MailDataset {
	day := func(n int) time.Time { return now.UTC().AddDate(0, 0, -n) }
	return &MailDataset{Mailbox: "INBOX", Scanned: 214, Invoices: []MailInvoice{
		{UID: 1, Date: day(4), Sender: demoHosting.Name, Addr: "billing@" + demoHosting.Domain, Domain: demoHosting.Domain,
			Subject: "Ihre Rechnung R0012345", Amount: demoHosting.Monthly, Attachments: []string{"Rechnung_R0012345.pdf"}},
		{UID: 2, Date: day(9), Sender: demoSoftware.Name, Addr: "sales@" + demoSoftware.Domain, Domain: demoSoftware.Domain,
			Subject: "Invoice for your order", Amount: 289, Attachments: []string{"invoice.pdf"}},
	}}
}

// DemoTrueNAS is the demo TrueNAS dataset.
func DemoTrueNAS() *TrueNASDataset {
	return &TrueNASDataset{URL: "https://nas.demo", Host: "truenas", Version: "25.04.2",
		Pools: []Pool{
			{Name: "tank", Status: "ONLINE", Healthy: true, Size: 16e12, Allocated: 14.1e12},
			{Name: "fast", Status: "DEGRADED", Healthy: false, Size: 2e12, Allocated: 0.6e12},
		},
		Alerts: []TNAlert{{ID: "a1", Level: "WARNING", Text: "Device /dev/sdc is causing slow I/O on pool fast."}},
		Apps:   []TNApp{{Name: "jellyfin", State: "RUNNING", Update: true}, {Name: "syncthing", State: "RUNNING"}},
		Snapshots: []SnapTask{
			{Dataset: "tank/photos", State: "FINISHED", Enabled: true, Last: time.Now().UTC().Add(-2 * time.Hour)},
			{Dataset: "fast/vms", State: "ERROR", Enabled: true, Last: time.Now().UTC().Add(-26 * time.Hour)},
		},
	}
}

// DemoKomodo is the demo Komodo dataset.
func DemoKomodo(now time.Time) *KomodoDataset {
	return &KomodoDataset{URL: "https://komodo.demo", ServersTotal: 3, ServersHealthy: 2, ServersProblem: 1,
		Stacks: []KStack{
			{Name: "immich", State: "running", Updates: []string{"immich-server", "immich-machine-learning"}},
			{Name: "paperless", State: "unhealthy"},
			{Name: "gitea", State: "running"},
		},
		Alerts: []KAlert{{Level: "CRITICAL", Kind: "ServerUnreachable", Name: "pi-backup", At: now.UTC().Add(-3 * time.Hour)}},
	}
}

// DemoPangolin is the demo Pangolin dataset.
func DemoPangolin() *PangolinDataset {
	on, off := true, false
	return &PangolinDataset{URL: "https://pangolin.demo/v1", Org: "home",
		Sites: []PSite{
			{Name: "homelab", Type: "newt", Online: &on, MBIn: 18400, MBOut: 92100, Update: true},
			{Name: "eltern", Type: "newt", Online: &off, MBIn: 120, MBOut: 340},
		},
		Resources: []PResource{
			{Name: "Immich", Domain: "photos.example.org", Enabled: true, Health: "healthy", SSO: true},
			{Name: "Vaultwarden", Domain: "vault.example.org", Enabled: true, Health: "unhealthy"},
		},
	}
}

// DemoAuthentik is the demo authentik dataset.
func DemoAuthentik(now time.Time) *AuthentikDataset {
	ago := func(d int) time.Time { return now.UTC().AddDate(0, 0, -d) }
	return &AuthentikDataset{URL: "https://auth.demo", Version: "2025.6.3", Latest: "2025.8.1", Outdated: true,
		Logins7d: 214, Logins24h: 31, Failed7d: 61, Failed24h: 38,
		Apps:   []AKApp{{Name: "Immich", Events: 96, Users: 4}, {Name: "Gitea", Events: 41, Users: 2}, {Name: "Andon", Events: 30, Users: 3}},
		Users:  []AKUser{{Name: "alex", LastLogin: ago(0)}, {Name: "sam", LastLogin: ago(2)}, {Name: "kim", LastLogin: ago(240)}, {Name: "test", LastLogin: time.Time{}}},
		Logins: []AKLogin{{User: "alex", IP: "203.0.113.7", Country: "DE", City: "Berlin", Lat: 52.52, Lon: 13.40, At: now.UTC().Add(-time.Hour)}},
		Failures: []AKLogin{{User: "admin", IP: "198.51.100.23", Country: "NL", City: "Amsterdam", At: now.UTC().Add(-20 * time.Minute)},
			{User: "alex", IP: "203.0.113.7", Country: "DE", City: "Berlin", At: now.UTC().Add(-3 * time.Hour)}},
	}
}

// DemoPihole is the demo Pi-hole dataset: blocking switched off.
func DemoPihole(now time.Time) *DNSFilterDataset {
	return &DNSFilterDataset{URL: "https://pihole.demo", Queries: 48210, Blocked: 9120, Percent: 18.9,
		Enabled: false, ListsUpdated: now.UTC().AddDate(0, 0, -21), Clients: 14,
		TopClients: []DNSClient{{IP: "192.168.1.20", Name: "laptop", Queries: 9120, Blocked: 1400}, {IP: "192.168.1.87", Queries: 14200, Blocked: 8700}},
		TopBlocked: []DNSDomain{{Domain: "telemetry.tv.example", Count: 6100}, {Domain: "ads.example.net", Count: 1900}}}
}

// DemoAdGuard is the demo AdGuard Home dataset.
func DemoAdGuard() *DNSFilterDataset {
	return &DNSFilterDataset{URL: "https://adguard.demo", Queries: 30500, Blocked: 4100, Percent: 13.4, Enabled: true,
		TopClients: []DNSClient{{IP: "192.168.1.31", Queries: 8800}, {IP: "192.168.1.12", Queries: 5100}},
		TopBlocked: []DNSDomain{{Domain: "metrics.app.example", Count: 1300}}}
}

// DemoNextcloud is the demo Nextcloud dataset.
func DemoNextcloud() *NextcloudDataset {
	return &NextcloudDataset{URL: "https://cloud.demo", Version: "31.0.8.1", FreeBytes: 7.5 * (1 << 30), Users: 6, Active24: 3,
		Files: 182340, AppUpdates: 4}
}

// DemoSabnzbd is the demo Sabnzbd dataset.
func DemoSabnzbd(now time.Time) *SabnzbdDataset {
	return &SabnzbdDataset{URL: "https://sab.demo", Slots: 3, SpeedKB: 42000, FreeGB: 14.2,
		Failures: []SabFailure{{Name: "Linux.ISO.2026", Reason: "Unpacking failed, CRC error", At: now.UTC().Add(-5 * time.Hour)}},
		Queue: []SabItem{{Name: "Debian.13.netinst", Percent: 64, Left: "0:03:10"}, {Name: "Podcast.Archive.2025", Percent: 12, Left: "0:41:55"},
			{Name: "Photos.Backup", Percent: 0, Left: "1:20:00"}}}
}

// DemoGluetun is the demo Gluetun dataset: tunnel up, wrong country.
func DemoGluetun() *GluetunDataset {
	return &GluetunDataset{URL: "http://gluetun.demo:8000", Status: "running", ExitIP: "185.65.134.10", Country: "Netherlands",
		OwnIP: "93.184.216.34", ExpectedCountry: "Sweden"}
}

// DemoDomains is the demo domain dataset.
func DemoDomains(now time.Time) *DomainsDataset {
	return &DomainsDataset{Domains: []DomainInfo{
		{Name: "example.de", MailChecked: true, SPF: true, DMARC: true},
		{Name: "example.org", Expires: now.UTC().AddDate(0, 0, 18), MailChecked: true, SPF: true},
	}}
}

// DemoBlacklist is the demo blacklist dataset.
func DemoBlacklist() *BlacklistDataset {
	return &BlacklistDataset{Checked: []string{"93.184.216.34"},
		Listings: []Listing{{IP: "93.184.216.34", Zone: "bl.spamcop.net", Code: "127.0.0.2"}}}
}

// demoTimer is a timer on demo project i (Kimai id i+1, as in DemoKimai).
func demoTimer(i int, activityID int64, activity string, begin time.Time) KimaiTimer {
	project := demoWorld.Project(demoProjectIDs[i])
	t := KimaiTimer{ProjectID: int64(i + 1), ActivityID: activityID, Project: project.Name.DE(), Activity: activity,
		Customer: demoCustomers[i].Name, Color: project.Color, Begin: begin}
	if !begin.IsZero() {
		t.ID = 901
	}
	return t
}

// DemoContract is a 40-hour week, Monday to Friday.
func DemoContract() *WorkContract {
	const day = 8 * 60
	return &WorkContract{Day: [7]int{day, day, day, day, day, 0, 0}}
}

// DemoKimaiLive is the demo live Kimai view: one timer running.
func DemoKimaiLive(now time.Time) *KimaiLive {
	begin := now.Add(-47 * time.Minute)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	at := func(h, m int) time.Time { return day.Add(time.Duration(h)*time.Hour + time.Duration(m)*time.Minute) }
	return &KimaiLive{URL: "https://kimai.demo", TodayMin: 312, WeekMin: 1590, Contract: DemoContract(),
		Active: []KimaiTimer{demoTimer(1, 7, demoEdit, begin)},
		Recent: []KimaiTimer{
			demoTimer(1, 7, demoEdit, time.Time{}),
			demoTimer(0, 2, demoMeeting, time.Time{}),
		},
		Today: []KimaiSpan{{Begin: at(9, 5), End: at(11, 40)}, {Begin: at(12, 15), End: at(13, 5)}}}
}

// DemoKimaiCatalog is the demo add-entry choice: two projects, one global
// and one project activity.
func DemoKimaiCatalog() *KimaiCatalog {
	return &KimaiCatalog{
		Projects:   []KimaiPick{{ID: 2, Name: demoProjectName(1), Customer: demoCustomers[1].Name}, {ID: 1, Name: demoProjectName(0), Customer: demoCustomers[0].Name}},
		Activities: []KimaiActivityPick{{ID: 7, Name: demoEdit}, {ID: 2, Name: demoMeeting, ProjectID: 1}},
	}
}
