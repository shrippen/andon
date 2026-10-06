package sources

import (
	"math/rand"
	"strconv"
	"strings"
	"time"

	"andon/internal/sources/demoworld"
)

// Generated demo datasets for demo:// connections, relative to today and
// deterministic. Every value comes from Studio Weber, the demo world shared
// by all shrippen projects (package demoworld); this file only shapes it.
// The datasets fit together so every rule fires once (world "bookkeeping"):
//
//	Northlight Pictures  visited on the skip day without a Kimai entry, unbilled hours, overdue invoice
//	Speiche              budget at 85 %, quote without reaction
//	Donaulicht Film      EU client (Austria), invoice at 0 % without VAT id

// demoBook is the world's bookkeeping scenario.
type demoBook struct {
	Seed      int64
	Rate, VAT float64
	Currency  string
	URLs      struct{ Time, Invoices string }
	Customers []string
	Projects  []string
	Countries map[string]string
	Time      struct {
		HistoryDays, ClientWindow, SkipDay int
		UnbilledDays, ExportedAfterDays    int
		RunningHours                       int
		Hours                              [2]int
		ClientDays                         []int
		Mix                                []int64
		OnSite, Edit, Visit, Meeting       string
		RunningID, RunningCustomer         int64
	}
	ContractMinutes [7]int
	Budgets         []KimaiProject
	Absences        []KimaiAbsence
	Invoices        struct {
		Months, IssuedDay, DueDays, PaidDays int
		Net                                  [2]float64
		Number                               string
		Overdue                              struct {
			FromEnd                 int
			Due, Reminded, NextSend string
		}
		Draft     NinjaInvoice
		Quote     NinjaQuote
		Recurring NinjaRecurring
	}
	Live struct {
		TodayMin, WeekMin, RunningMin int
		Spans                         []KimaiSpan
		Activities                    struct{ Edit, Meeting int64 }
		TimerID                       int64
		SheetIDs                      [2]int64
		Tags                          []string
	}
}

// bookOf reads the scenario with its dates around now.
func bookOf(now time.Time) *demoBook {
	b := &demoBook{}
	demoworld.MustDecode("bookkeeping", now, b)
	return b
}

var (
	demoWorld = demoworld.Get()
	book      = bookOf(time.Now())
	// Customers and projects 1-3 in every demo dataset: on-site client,
	// budget client, EU client.
	demoCustomers = demoKimaiCustomers()
	demoEdit      = demoWorld.Activity(book.Time.Edit).Name.DE()
	demoOnSite    = demoWorld.Activity(book.Time.Visit).Name.DE() + " " + book.Time.OnSite
	demoMeeting   = demoWorld.Activity(book.Time.Meeting).Name.DE()
)

func demoKimaiCustomers() []KimaiCustomer {
	out := make([]KimaiCustomer, len(book.Customers))
	for i, id := range book.Customers {
		out[i] = KimaiCustomer{ID: int64(i + 1), Name: demoWorld.Customer(id).Name}
	}
	return out
}

func demoProjectName(i int) string { return demoWorld.Project(book.Projects[i]).Name.DE() }

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
	days := weekdaysBack(today, book.Time.ClientWindow)
	limit := book.Time.ClientDays[len(book.Time.ClientDays)-1] + 1
	if len(days) > limit {
		days = days[:limit]
	}
	return days
}

// DemoKimai is the demo Kimai dataset.
func DemoKimai(now time.Time) *KimaiDataset {
	today := demoDay(now)
	b := bookOf(now)
	rnd := rand.New(rand.NewSource(b.Seed))
	clients := clientDays(today)
	visits := map[time.Time]bool{}
	for _, i := range b.Time.ClientDays {
		if i < len(clients) {
			visits[clients[i]] = true
		}
	}
	skip := clients[b.Time.SkipDay]

	history := weekdaysBack(today, b.Time.HistoryDays)
	sheets := make([]KimaiSheet, 0, len(history))
	for i := len(history) - 1; i >= 0; i-- {
		d := history[i]
		customer := b.Time.Mix[rnd.Intn(len(b.Time.Mix))]
		if visits[d] {
			customer = 1
		}
		if d.Equal(skip) {
			customer = 2
		}
		hours := b.Time.Hours[0] + rnd.Intn(b.Time.Hours[1]-b.Time.Hours[0]+1)
		age := int(today.Sub(d).Hours() / 24)
		activity := demoEdit
		if visits[d] {
			activity = demoOnSite
		}
		sheets = append(sheets, KimaiSheet{
			ID: int64(len(sheets) + 1), Begin: stamp(d, 9, 0), End: stamp(d, 9+hours, 0),
			Minutes: hours * 60, Rate: float64(hours) * b.Rate, Billable: true,
			Exported:  !(customer == 1 && age <= b.Time.UnbilledDays) && age > b.Time.ExportedAfterDays,
			ProjectID: customer, CustomerID: customer, Activity: activity, UserID: 1,
		})
	}

	projects := make([]KimaiProject, len(b.Projects))
	for i := range b.Projects {
		p := KimaiProject{}
		if i < len(b.Budgets) {
			p = b.Budgets[i]
		}
		p.ID, p.Name, p.CustomerID = int64(i+1), demoProjectName(i), int64(i+1)
		projects[i] = p
	}

	running := now.UTC().Add(-time.Duration(b.Time.RunningHours) * time.Hour)
	return &KimaiDataset{
		URL:        b.URLs.Time,
		Contract:   DemoContract(),
		Timesheets: sheets,
		Active: []KimaiSheet{{ID: b.Time.RunningID, Begin: running.Format(time.RFC3339), Billable: true,
			ProjectID: b.Time.RunningCustomer, CustomerID: b.Time.RunningCustomer, Activity: demoEdit, UserID: 1}},
		Projects:      projects,
		Customers:     demoCustomers,
		Absences:      b.Absences,
		Holidays:      demoHolidays(today),
		HolidayBundle: true,
		Mileage:       true,
		PlacesWrite:   true,
		Places:        demoKimaiPlaces(now),
		MileageTrips:  demoMileageTrips(now),
	}
}

// demoHolidays are the world's public holidays of today's year.
func demoHolidays(today time.Time) []KimaiHoliday {
	var dates map[string]string
	demoworld.MustDecode("public_holidays.dates", today, &dates)
	var out []KimaiHoliday
	for day, name := range dates {
		if strings.HasPrefix(day, strconv.Itoa(today.Year())) {
			out = append(out, KimaiHoliday{Date: day, Name: name})
		}
	}
	return out
}

// DemoNinja is the demo Invoice Ninja dataset.
func DemoNinja(now time.Time) *NinjaDataset {
	today := demoDay(now)
	b := bookOf(now)
	inv := b.Invoices
	rnd := rand.New(rand.NewSource(b.Seed))
	var invoices []NinjaInvoice
	var payments []NinjaPayment
	number := int64(1)

	for back := inv.Months; back > 0; back-- {
		start := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, -back, 0)
		for _, c := range demoCustomers {
			net := round2(inv.Net[0] + rnd.Float64()*(inv.Net[1]-inv.Net[0]))
			tax := round2(net * b.VAT)
			if c.ID == 3 && back == 1 {
				tax = 0
			}
			issued := start.AddDate(0, 0, inv.IssuedDay-1)
			invoices = append(invoices, NinjaInvoice{
				ID: number, Number: demoNumber(inv.Number, issued, number), ClientID: c.ID, Status: "paid",
				Date: iso(issued), DueDate: iso(issued.AddDate(0, 0, inv.DueDays)), Amount: net + tax, Taxes: tax, Net: net,
			})
			payments = append(payments, NinjaPayment{ID: number, Date: iso(issued.AddDate(0, 0, inv.PaidDays)), Amount: net + tax, ClientID: c.ID})
			number++
		}
	}

	overdue := &invoices[len(invoices)-inv.Overdue.FromEnd]
	overdue.Status, overdue.Balance, overdue.DueDate = "sent", overdue.Amount, inv.Overdue.Due
	overdue.Reminded, overdue.NextSend = inv.Overdue.Reminded, inv.Overdue.NextSend
	kept := payments[:0]
	for _, p := range payments {
		if p.ID != overdue.ID {
			kept = append(kept, p)
		}
	}
	draft := inv.Draft
	draft.ID = number
	quote := inv.Quote
	quote.Number = demoNumber(quote.Number, today, 0)

	return &NinjaDataset{
		URL: b.URLs.Invoices, Currency: b.Currency, Invoices: append(invoices, draft), Payments: kept,
		Clients:       demoNinjaClients(b),
		Expenses:      demoNinjaExpenses(today, b),
		Quotes:        []NinjaQuote{quote},
		Recurring:     []NinjaRecurring{inv.Recurring},
		HomeCountryID: b.Countries["DE"],
	}
}

// demoNumber fills a number pattern: "R-{year}-{n}" → "R-2026-007".
func demoNumber(pattern string, issued time.Time, n int64) string {
	return strings.NewReplacer("{year}", issued.Format("2006"), "{n}", pad3(n)).Replace(pattern)
}

func demoNinjaClients(b *demoBook) []NinjaClient {
	out := make([]NinjaClient, len(b.Customers))
	for i, id := range b.Customers {
		c := demoWorld.Customer(id)
		out[i] = NinjaClient{ID: int64(i + 1), Name: c.Name, VATNumber: c.VATID, CountryID: b.Countries[c.Country]}
	}
	return out
}

// demoNinjaExpenses are the receipts of the demo world. Their days are
// offsets from Monday of the current week.
func demoNinjaExpenses(today time.Time, b *demoBook) []NinjaExpense {
	monday := demoMonday(today)
	out := make([]NinjaExpense, 0, len(demoWorld.Receipts))
	for _, r := range demoWorld.Receipts {
		out = append(out, NinjaExpense{ID: int64(r.ID), Date: iso(monday.AddDate(0, 0, r.Day)), Amount: r.Amount,
			Tax: round2(r.Amount * b.VAT / (1 + b.VAT)), Notes: r.Vendor + " · " + r.Note.DE(), VendorID: int64(r.ID)})
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

// DemoSnipe is the demo Snipe-IT dataset: the world's assets and licences
// with the states of "assets_state".
func DemoSnipe(now time.Time) *SnipeDataset {
	data := &SnipeDataset{}
	demoworld.MustDecode("assets_state", now, data)
	for i := range data.Assets {
		a, s := demoWorld.Inventory.Assets[i], &data.Assets[i]
		s.ID, s.Name, s.Tag, s.Model, s.Category, s.PurchaseCost = int64(i+1), a.Name, a.Tag, a.Model, a.Category.DE(), a.Cost
	}
	for i := range data.Licenses {
		l, s := demoWorld.Inventory.Licenses[i], &data.Licenses[i]
		s.ID, s.Name, s.Seats = int64(i+1), l.Name, l.Seats
	}
	return data
}

// demoLocation is the world's Dawarich setup.
type demoLocation struct {
	URL, Home, Site string
	Person          string // whose tracks these are
	Visit           struct {
		From, To string
		Minutes  int
	}
	Areas []struct {
		ID     int64
		Place  string
		Radius float64
	}
	Stats struct {
		TotalKm, YearKm, Countries, Cities, MonthKm, LastMonthKm float64
	}
	LastPoint string
	Route     struct{ Steps, StepMinutes int }
	// Day is a weekday, Weekend a Saturday as tracks: [from, to, "07:30",
	// "07:50", mode].
	Day     struct{ Segments [][5]string }
	Weekend struct {
		Weekday  int
		Segments [][5]string
	}
}

// demoLogbook is how the world books trips (Kimai Anfahrten).
type demoLogbook struct {
	PlaceRadius  float64
	MinutesPerKm float64
	RoadFactor   float64
	Times        struct{ Out, Back string }
}

// demoTrip is a trip of the world's mileage plugin.
type demoTrip struct {
	User, From, To, Project string
	Day                     int
	KM                      float64
}

func locationOf(now time.Time) *demoLocation {
	l := &demoLocation{}
	demoworld.MustDecode("location", now, l)
	return l
}

// demoClock is "08:30" as hour and minute.
func demoClock(hhmm string) (int, int) {
	h, _ := strconv.Atoi(hhmm[:2])
	m, _ := strconv.Atoi(hhmm[3:])
	return h, m
}

// DemoDawarich is the demo Dawarich dataset: a visit at the site on each
// client day.
func DemoDawarich(now time.Time) *DawarichDataset {
	loc := locationOf(now)
	site := demoWorld.Place(loc.Site)
	days := clientDays(demoDay(now))
	fromH, fromM := demoClock(loc.Visit.From)
	toH, toM := demoClock(loc.Visit.To)
	var visits []DawarichVisit
	for _, i := range book.Time.ClientDays {
		if i >= len(days) {
			continue
		}
		lat, lon := site.Lat, site.Lon
		visits = append(visits, DawarichVisit{ID: int64(i), Start: stamp(days[i], fromH, fromM), End: stamp(days[i], toH, toM),
			Minutes: loc.Visit.Minutes, AreaID: 2, Name: site.Name.DE(), Lat: &lat, Lon: &lon})
	}
	var areas []DawarichArea
	for _, a := range loc.Areas {
		p := demoWorld.Place(a.Place)
		areas = append(areas, DawarichArea{ID: a.ID, Name: p.Name.DE(), Lat: p.Lat, Lon: p.Lon, Radius: a.Radius})
	}
	return &DawarichDataset{
		URL:    loc.URL,
		Areas:  areas,
		Visits: visits,
		Stats: map[string]any{"totalDistanceKm": loc.Stats.TotalKm, "yearlyStats": []any{map[string]any{
			"year": float64(now.Year()), "totalDistanceKm": loc.Stats.YearKm, "totalCountriesVisited": loc.Stats.Countries,
			"totalCitiesVisited": loc.Stats.Cities,
			"monthlyDistanceKm": map[string]any{
				strings.ToLower(now.Month().String()):                   loc.Stats.MonthKm,
				strings.ToLower(now.AddDate(0, -1, 0).Month().String()): loc.Stats.LastMonthKm,
			},
		}}},
		LastPoint:   loc.LastPoint,
		Tracks:      demoTracks(now, tracksFrom(now.UTC(), visitDays)),
		TracksState: TracksOK,
	}
}

// DemoDawarichRoute is the track of the demo visits between from and to:
// home to the site before each visit and back after it, a point every
// few minutes along the straight line.
func DemoDawarichRoute(from, to, now time.Time) *DawarichRoute {
	loc := locationOf(now)
	home, site := demoWorld.Place(loc.Home), demoWorld.Place(loc.Site)
	steps, pace := loc.Route.Steps, time.Duration(loc.Route.StepMinutes)*time.Minute
	out := &DawarichRoute{}
	leg := func(a, b demoworld.Place, start time.Time) {
		for i := range steps + 1 {
			f := float64(i) / float64(steps)
			out.Points = append(out.Points, RoutePoint{Lat: a.Lat + (b.Lat-a.Lat)*f, Lon: a.Lon + (b.Lon-a.Lon)*f,
				At: start.Add(time.Duration(i) * pace)})
		}
	}
	for _, v := range DemoDawarich(now).Visits {
		begin, _ := time.Parse(time.RFC3339, v.Start)
		end, _ := time.Parse(time.RFC3339, v.End)
		if begin.Before(from) || !begin.Before(to) {
			continue
		}
		leg(home, site, begin.Add(-time.Duration(steps)*pace))
		leg(site, home, end)
	}
	return out
}

// DemoKuma is the demo Uptime Kuma dataset.
func DemoKuma() *KumaDataset {
	data := &KumaDataset{}
	demoworld.MustDecode("monitoring", time.Now(), data)
	return data
}

// DemoGlances is the demo Glances host: busy CPU, one disk filling up.
func DemoGlances() *GlancesResult {
	data := &GlancesResult{}
	demoworld.MustDecode("server", time.Now(), data)
	return data
}

// DemoGlancesDetail is the demo host's processes, sensors and network.
func DemoGlancesDetail() *GlancesDetail {
	data := &GlancesDetail{}
	demoworld.MustDecode("server", time.Now(), data)
	return data
}

// DemoGlancesHistory is one demo metric over the last hour, a sample a minute.
func DemoGlancesHistory(now time.Time, metric string, points int) *GlancesHistory {
	var curve struct{ Base, Swing float64 }
	demoworld.MustDecode("server.history", now, &curve)
	rnd := rand.New(rand.NewSource(book.Seed))
	out := &GlancesHistory{Metric: metric}
	for i := points; i > 0; i-- {
		at := now.UTC().Add(-time.Duration(i) * time.Minute).Format("2006-01-02T15:04:05")
		out.Samples = append(out.Samples, Sample{At: at, Value: curve.Base + rnd.Float64()*curve.Swing})
	}
	return out
}

// DemoProxmox is the demo Proxmox VE dataset.
func DemoProxmox(now time.Time) *ProxmoxDataset {
	data := &ProxmoxDataset{}
	demoworld.MustDecode("virtualization", now, data)
	return data
}

// DemoPaperless is the demo Paperless-ngx dataset.
func DemoPaperless(now time.Time) *PaperlessDataset {
	data := &PaperlessDataset{}
	demoworld.MustDecode("documents", now, data)
	return data
}

// DemoCerts is the demo certificate dataset.
func DemoCerts(now time.Time) *CertDataset {
	data := &CertDataset{}
	demoworld.MustDecode("certs", now, data)
	return data
}

// DemoScrutiny is the demo Scrutiny dataset.
func DemoScrutiny(now time.Time) *ScrutinyDataset {
	data := &ScrutinyDataset{}
	demoworld.MustDecode("disk_health", now, data)
	return data
}

// DemoImmich is the demo Immich dataset.
func DemoImmich() *ImmichDataset {
	data := &ImmichDataset{}
	demoworld.MustDecode("photos", time.Now(), data)
	return data
}

// DemoUmamiDetail is the demo sites' pages, referrers and days.
func DemoUmamiDetail(now time.Time) *UmamiDetail {
	var sites map[string]struct {
		UmamiSite
		DayBase int
	}
	demoworld.MustDecode("sites.detail", now, &sites)
	out := &UmamiDetail{Sites: map[string]UmamiSite{}}
	for id, s := range sites {
		for i := 29; i >= 0; i-- {
			s.Days = append(s.Days, Count{Name: now.AddDate(0, 0, -i).Format(time.DateOnly), N: s.DayBase + (i*7)%23})
		}
		out.Sites[id] = s.UmamiSite
	}
	return out
}

// DemoUmami is the demo Umami dataset.
func DemoUmami() *UmamiDataset {
	data := &UmamiDataset{}
	demoworld.MustDecode("sites", time.Now(), data)
	return data
}

// DemoFreshRSS is the demo FreshRSS dataset.
func DemoFreshRSS(now time.Time) *FreshRSSDataset {
	data := &FreshRSSDataset{}
	demoworld.MustDecode("feeds", now, data)
	return data
}

// DemoGitea is the demo Gitea dataset.
func DemoGitea(now time.Time) *GiteaDataset {
	data := &GiteaDataset{}
	demoworld.MustDecode("code.gitea", now, data)
	return data
}

// DemoBorg is the demo Borg Backup Server dataset.
func DemoBorg(now time.Time) *BorgDataset {
	data := &BorgDataset{}
	demoworld.MustDecode("backups.server", now, data)
	return data
}

// DemoHass is the demo Home Assistant dataset.
func DemoHass(now time.Time) *HassDataset {
	data := &HassDataset{}
	demoworld.MustDecode("smart_home", now, data)
	return data
}

// DemoGiteaActivity is the demo's commits per week and its GitHub mirror.
func DemoGiteaActivity(now time.Time) *GiteaActivity {
	data := &GiteaActivity{}
	demoworld.MustDecode("code.gitea", now, data)
	return data
}

// DemoHassHistory is a day of the demo entities: the living room warms
// in the morning, the office light goes on and off.
func DemoHassHistory(now time.Time, ids []string) *HassHistory {
	var curves struct {
		Temperature struct {
			Entity      string
			Base, Swing float64
		}
		Light struct {
			Entity       string
			OnFrom, OnTo int
		}
	}
	demoworld.MustDecode("smart_home.history", now, &curves)
	out := &HassHistory{ByID: map[string][]HassPoint{}}
	start := now.UTC().Add(-HassHistoryHours * time.Hour)
	for _, id := range ids {
		for h := range HassHistoryHours {
			at := start.Add(time.Duration(h) * time.Hour)
			switch id {
			case curves.Temperature.Entity:
				t := curves.Temperature.Base + curves.Temperature.Swing*float64((at.Hour()+18)%24)/24
				out.ByID[id] = append(out.ByID[id], HassPoint{At: at, State: strconv.FormatFloat(t, 'f', 1, 64)})
			case curves.Light.Entity:
				on := at.Hour() >= curves.Light.OnFrom && at.Hour() < curves.Light.OnTo
				out.ByID[id] = append(out.ByID[id], HassPoint{At: at, State: map[bool]string{true: HassOn, false: "off"}[on]})
			}
		}
	}
	return out
}

// DemoSure is the demo Sure dataset; one income matches the open demo
// invoice, one expected payment is overdue.
func DemoSure(now time.Time) *SureDataset {
	var data struct {
		SureDataset
		Transactions []struct {
			SureTxn
			Expense bool
		}
	}
	demoworld.MustDecode("bank", now, &data)
	out := data.SureDataset
	for _, t := range data.Transactions {
		if t.Expense {
			t.Amount = -t.Amount
		}
		out.Transactions = append(out.Transactions, t.SureTxn)
	}
	return &out
}

// DemoLinkwarden is the demo Linkwarden dataset: two bookmarks match
// Homelab tiles, one is missing there.
func DemoLinkwarden() *LinkwardenDataset {
	data := &LinkwardenDataset{}
	demoworld.MustDecode("bookmarks", time.Now(), data)
	return data
}

// DemoKintsugi is the demo Kintsugi dataset: three open suggestions, the
// oldest a week old, and a failed last run.
func DemoKintsugi(now time.Time) *KintsugiDataset {
	data := &KintsugiDataset{}
	demoworld.MustDecode("suggestions", now, data)
	return data
}

// DemoPGBack is the demo PG Back Web dataset.
func DemoPGBack(now time.Time) *PGBackDataset {
	data := &PGBackDataset{}
	demoworld.MustDecode("backups.databases", now, data)
	return data
}

// DemoMail is the demo mailbox: one invoice matches a demo expense
// amount, one does not.
func DemoMail(now time.Time) *MailDataset {
	data := &MailDataset{}
	demoworld.MustDecode("mail", now, data)
	return data
}

// DemoTrueNAS is the demo TrueNAS dataset.
func DemoTrueNAS() *TrueNASDataset {
	data := &TrueNASDataset{}
	demoworld.MustDecode("storage", time.Now(), data)
	return data
}

// DemoTrueNASDatasets are the demo pools' largest datasets.
func DemoTrueNASDatasets() *TrueNASDatasets {
	var data struct{ Datasets []TNDataset }
	demoworld.MustDecode("storage", time.Now(), &data)
	return &TrueNASDatasets{List: data.Datasets}
}

// DemoKomodo is the demo Komodo dataset.
func DemoKomodo(now time.Time) *KomodoDataset {
	data := &KomodoDataset{}
	demoworld.MustDecode("stacks", now, data)
	return data
}

// DemoKomodoDetail is the demo servers' load and the stacks' last
// deployments.
func DemoKomodoDetail(now time.Time) *KomodoDetail {
	data := &KomodoDetail{}
	demoworld.MustDecode("stacks", now, data)
	return data
}

// DemoPangolin is the demo Pangolin dataset.
func DemoPangolin() *PangolinDataset {
	data := &PangolinDataset{}
	demoworld.MustDecode("tunnel", time.Now(), data)
	return data
}

// DemoAuthentik is the demo authentik dataset.
func DemoAuthentik(now time.Time) *AuthentikDataset {
	var data struct {
		AuthentikDataset
		PerDay [][2]int
	}
	demoworld.MustDecode("identity", now, &data)
	out := data.AuthentikDataset
	for i, d := range data.PerDay {
		day := now.UTC().AddDate(0, 0, i-len(data.PerDay)+1).Format(time.DateOnly)
		out.Days = append(out.Days, AKDay{Day: day, Logins: d[0], Failed: d[1]})
	}
	return &out
}

// DemoPihole is the demo Pi-hole dataset: blocking switched off.
func DemoPihole(now time.Time) *DNSFilterDataset {
	return demoDNS("dns.pihole", now)
}

// demoDNS is a DNS filter from the world; its hourly counts follow the
// curve [base, swing].
func demoDNS(path string, now time.Time) *DNSFilterDataset {
	var data struct {
		DNSFilterDataset
		Curve struct{ Queries, Blocked [2]int }
	}
	demoworld.MustDecode(path, now, &data)
	out := data.DNSFilterDataset
	out.Hourly, out.HourlyBlocked = demoHours(data.Curve.Queries), demoHours(data.Curve.Blocked)
	return &out
}

// demoHours is a day of hourly counts: quiet at night, busy evenings.
func demoHours(curve [2]int) []int {
	base, swing := curve[0], curve[1]
	out := make([]int, 24)
	for h := range out {
		out[h] = base + swing*((h+6)%24)/24
		if h < 6 {
			out[h] = base / 4
		}
	}
	return out
}

// DemoAdGuard is the demo AdGuard Home dataset.
func DemoAdGuard() *DNSFilterDataset {
	return demoDNS("dns.adguard", time.Now())
}

// DemoNextcloud is the demo Nextcloud dataset.
func DemoNextcloud() *NextcloudDataset {
	data := &NextcloudDataset{}
	demoworld.MustDecode("cloud", time.Now(), data)
	return data
}

// DemoSabnzbd is the demo Sabnzbd dataset.
func DemoSabnzbd(now time.Time) *SabnzbdDataset {
	data := &SabnzbdDataset{}
	demoworld.MustDecode("downloads", now, data)
	return data
}

// DemoSabStats is the demo SABnzbd volume: four weeks, quieter weekends.
func DemoSabStats(now time.Time) *SabStats {
	const gb = 1e9
	var stats struct {
		Days    int
		Servers map[string]float64
	}
	demoworld.MustDecode("downloads.stats", now, &stats)
	out := &SabStats{Daily: map[string]float64{}, Servers: stats.Servers}
	for i := range stats.Days {
		day := now.AddDate(0, 0, -i)
		v := float64(2+i%5) * gb
		if wd := day.Weekday(); wd == time.Saturday || wd == time.Sunday {
			v /= 2
		}
		out.Daily[day.Format(time.DateOnly)] = v
		out.Month += v
		if i < 7 {
			out.Week += v
		}
	}
	out.Day, out.Total = out.Daily[now.Format(time.DateOnly)], out.Month*12
	return out
}

// DemoGluetun is the demo Gluetun dataset: tunnel up, wrong country.
func DemoGluetun() *GluetunDataset {
	data := &GluetunDataset{}
	demoworld.MustDecode("vpn", time.Now(), data)
	return data
}

// DemoDomains is the demo domain dataset.
func DemoDomains(now time.Time) *DomainsDataset {
	data := &DomainsDataset{}
	demoworld.MustDecode("domains", now, data)
	return data
}

// DemoBlacklist is the demo blacklist dataset.
func DemoBlacklist() *BlacklistDataset {
	data := &BlacklistDataset{}
	demoworld.MustDecode("mail_blacklist", time.Now(), data)
	return data
}

// demoTimer is a timer on demo project i (Kimai id i+1, as in DemoKimai).
func demoTimer(i int, activityID int64, activity string, begin time.Time) KimaiTimer {
	project := demoWorld.Project(book.Projects[i])
	t := KimaiTimer{ProjectID: int64(i + 1), ActivityID: activityID, Project: project.Name.DE(), Activity: activity,
		Customer: demoCustomers[i].Name, Color: project.Color, Begin: begin}
	if !begin.IsZero() {
		t.ID = book.Live.TimerID
	}
	return t
}

// DemoContract is a 40-hour week, Monday to Friday.
func DemoContract() *WorkContract {
	return &WorkContract{Day: book.ContractMinutes}
}

// DemoKimaiLive is the demo live Kimai view: one timer running.
func DemoKimaiLive(now time.Time) *KimaiLive {
	b := bookOf(now)
	edit, meeting := b.Live.Activities.Edit, b.Live.Activities.Meeting
	begin := now.Add(-time.Duration(b.Live.RunningMin) * time.Minute)
	return &KimaiLive{URL: b.URLs.Time, TodayMin: b.Live.TodayMin, WeekMin: b.Live.WeekMin, Contract: DemoContract(),
		Active: []KimaiTimer{demoTimer(1, edit, demoEdit, begin)},
		Recent: []KimaiTimer{
			demoTimer(1, edit, demoEdit, time.Time{}),
			demoTimer(0, meeting, demoMeeting, time.Time{}),
		},
		Today: b.Live.Spans}
}

// DemoKimaiDay is the demo day list: the live view's two blocks plus the
// running timer.
func DemoKimaiDay(now time.Time) *KimaiDay {
	b := bookOf(now)
	edit, meeting := b.Live.Activities.Edit, b.Live.Activities.Meeting
	spans := b.Live.Spans
	first, second := demoTimer(0, meeting, demoMeeting, spans[0].Begin), demoTimer(1, edit, demoEdit, spans[1].Begin)
	first.ID, first.End, first.Billable = b.Live.SheetIDs[0], spans[0].End, true
	second.ID, second.End, second.Tags = b.Live.SheetIDs[1], spans[1].End, b.Live.Tags
	running := demoTimer(1, edit, demoEdit, now.Add(-time.Duration(b.Live.RunningMin)*time.Minute))
	return &KimaiDay{Sheets: []KimaiTimer{first, second, running}}
}

// DemoKimaiCatalog is the demo add-entry choice: two projects, one global
// and one project activity.
func DemoKimaiCatalog() *KimaiCatalog {
	edit, meeting := book.Live.Activities.Edit, book.Live.Activities.Meeting
	return &KimaiCatalog{
		Projects:   []KimaiPick{{ID: 2, Name: demoProjectName(1), Customer: demoCustomers[1].Name}, {ID: 1, Name: demoProjectName(0), Customer: demoCustomers[0].Name}},
		Activities: []KimaiActivityPick{{ID: edit, Name: demoEdit}, {ID: meeting, Name: demoMeeting, ProjectID: 1}},
	}
}
