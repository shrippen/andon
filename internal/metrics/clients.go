package metrics

import (
	"math"
	"sort"
	"time"

	"andon/internal/sources"
)

// ClientCard is one Kimai customer with its Invoice Ninja client (joined
// by name): the numbers of the customer page.
type ClientCard struct {
	CustomerID  int64
	Name        string
	Matched     bool    // an Invoice Ninja client of the same name exists
	Hours       float64 // worked in the span
	Unbilled    float64 // billable, not exported work, as of today
	Open        float64 // balance of open invoices, as of today
	Overdue     float64
	Revenue     float64 // net of the span's counted invoices
	PaymentDays int     // typical days to payment, 0 without history
	LastWork    string  // day of the newest time entry, "" if none
	Invoices    []NinjaOpenInvoice
	Projects    []ClientProject

	// The customer's state, whatever the span: revenue of the last half
	// year against the one before, overdue money, how long it is quiet.
	Recent, Prior float64
	Health        ClientHealth

	// The hourly rate billed lately and since when; a raise is due after
	// raiseMonths, Raise is the rate grown by inflation since.
	Rate      float64
	RateSince string
	Raise     float64

	Recalc []ProjectRecalc // the projects over their whole life
}

// ClientHealth is a customer's state and why: Signals are i18n keys
// under "clients.signal_".
type ClientHealth struct {
	State   ClientState
	Signals []string
}

// ClientState ranks a customer.
type ClientState string

const (
	ClientOK    ClientState = "ok"
	ClientWatch ClientState = "watch"
	ClientRisk  ClientState = "risk"
)

// A rate unchanged this long is due for a raise; Inflation is the
// yearly rise it is grown by (an assumption, shown as such).
const (
	raiseMonths = 12
	Inflation   = 0.02
)

// Health thresholds: half years compared, a drop or rise by this share
// counts, so many days without work is quiet.
const (
	halfYearMonths = 6
	trendShare     = 0.4
	quietDays      = 90
)

// ClientProject is a project's hours in the span.
type ClientProject struct {
	Name  string
	Hours float64
}

// ClientCards builds a card per Kimai customer, most hours in span
// first. ninja may be nil.
func ClientCards(kimai *sources.KimaiDataset, ninja *sources.NinjaDataset, today time.Time, center Center, m ClientMap, span Span) []ClientCard {
	projects := map[int64]*sources.KimaiProject{}
	for i := range kimai.Projects {
		projects[kimai.Projects[i].ID] = &kimai.Projects[i]
	}
	cards := map[int64]*ClientCard{}
	projectHours := map[int64]map[string]float64{}
	for _, c := range kimai.Customers {
		cards[c.ID] = &ClientCard{CustomerID: c.ID, Name: c.Name}
		projectHours[c.ID] = map[string]float64{}
	}

	rates := map[int64][]sources.KimaiSheet{}
	for _, s := range kimai.Timesheets {
		card, ok := cards[s.CustomerID]
		if !ok || len(s.Begin) < len("2006-01-02") {
			continue
		}
		day := s.Begin[:len("2006-01-02")]
		if s.Billable && s.HourlyRate > 0 {
			rates[s.CustomerID] = append(rates[s.CustomerID], s)
		}
		hours := float64(s.Minutes) / minutesPerHour
		if day > card.LastWork {
			card.LastWork = day
		}
		if s.Billable && !s.Exported && s.End != "" {
			card.Unbilled += s.Rate
		}
		if !span.HasDay(day) {
			continue
		}
		card.Hours += hours
		if p, ok := projects[s.ProjectID]; ok {
			projectHours[s.CustomerID][p.Name] += hours
		}
	}

	if ninja != nil {
		joinNinja(cards, ninja, today, center, m, span)
	}
	for id, card := range cards {
		card.Rate, card.RateSince = rateSince(rates[id])
		if since, ok := ParseDay(card.RateSince); ok && !since.After(today.AddDate(0, -raiseMonths, 0)) {
			years := today.Sub(since).Hours() / hoursPerDay / daysPerYear
			card.Raise = round2(card.Rate * math.Pow(1+Inflation, years))
		}
		card.Health = healthOf(*card, today)
		card.Recalc = Recalc(kimai, id)
	}

	out := make([]ClientCard, 0, len(cards))
	for id, card := range cards {
		for name, h := range projectHours[id] {
			card.Projects = append(card.Projects, ClientProject{Name: name, Hours: round2(h)})
		}
		sort.Slice(card.Projects, func(a, b int) bool { return card.Projects[a].Hours > card.Projects[b].Hours })
		card.Hours, card.Revenue = round2(card.Hours), round2(card.Revenue)
		out = append(out, *card)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Hours != out[b].Hours {
			return out[a].Hours > out[b].Hours
		}
		return out[a].Name < out[b].Name
	})
	return out
}

// joinNinja adds invoices, revenue and payment days of each customer's
// client (m: stored link, else the same name).
func joinNinja(cards map[int64]*ClientCard, ninja *sources.NinjaDataset, today time.Time, center Center, m ClientMap, span Span) {
	byClient := map[int64]*ClientCard{}
	for id, card := range cards {
		if c, ok := m.ClientOf(ninja, id, card.Name); ok {
			card.Matched = true
			byClient[c.ID] = card
		}
	}

	recent := today.AddDate(0, -halfYearMonths, 0).Format(time.DateOnly)
	prior := today.AddDate(0, -2*halfYearMonths, 0).Format(time.DateOnly)
	for _, inv := range ninja.Invoices {
		card, ok := byClient[inv.ClientID]
		if !ok || !isCounted(inv.Status) {
			continue
		}
		if span.HasDay(inv.Date) {
			card.Revenue += inv.Net
		}
		switch {
		case inv.Date >= recent:
			card.Recent += inv.Net
		case inv.Date >= prior:
			card.Prior += inv.Net
		}
	}
	for _, open := range NinjaOpenInvoices(ninja, today) {
		card, ok := byClient[open.ClientID]
		if !ok {
			continue
		}
		card.Open += open.Balance
		if open.OverdueDays > 0 {
			card.Overdue += open.Balance
		}
		card.Invoices = append(card.Invoices, open)
	}
	for id, days := range NinjaPaymentDays(ninja, center) {
		if card, ok := byClient[id]; ok {
			card.PaymentDays = days
		}
	}
}

// rateSince is the newest billable entry's hourly rate and the day it
// began: the first entry of the unbroken run at that rate, back from
// the newest.
func rateSince(sheets []sources.KimaiSheet) (float64, string) {
	if len(sheets) == 0 {
		return 0, ""
	}
	sort.Slice(sheets, func(a, b int) bool { return sheets[a].Begin > sheets[b].Begin })
	rate, since := sheets[0].HourlyRate, sheets[0].Begin
	for _, s := range sheets[1:] {
		if s.HourlyRate != rate {
			break
		}
		since = s.Begin
	}
	return rate, since[:min(len(since), len(time.DateOnly))]
}

// healthOf reads a customer's state from its card:
//
//	overdue money                          → risk
//	revenue down by trendShare, and quiet  → risk
//	revenue down, or quiet                 → watch
//	revenue up by trendShare               → ok, noted as growing
func healthOf(c ClientCard, today time.Time) ClientHealth {
	var h ClientHealth
	quiet := c.LastWork != "" && daysApart(c.LastWork, today) > quietDays
	down := c.Prior > 0 && c.Recent < c.Prior*(1-trendShare)
	if c.Overdue > 0 {
		h.Signals = append(h.Signals, "overdue")
	}
	if down {
		h.Signals = append(h.Signals, "declining")
	}
	if quiet {
		h.Signals = append(h.Signals, "quiet")
	}
	if c.Prior > 0 && c.Recent > c.Prior*(1+trendShare) {
		h.Signals = append(h.Signals, "growing")
	}
	if c.Raise > 0 {
		h.Signals = append(h.Signals, "raise")
	}
	switch {
	case c.Overdue > 0 || (down && quiet):
		h.State = ClientRisk
	case down || quiet:
		h.State = ClientWatch
	default:
		h.State = ClientOK
	}
	return h
}

// ProjectRecalc is a project's post-calculation over its whole life, as
// Kimai counts it: used money and time against the budget, and what an
// hour brought.
type ProjectRecalc struct {
	Name, End       string
	Hours, Used     float64 // hours worked, money billed by Kimai's rates
	Budget, BudgetH float64 // money and time budget, 0 none
	Share           float64 // used of the budget (money, else time), 0 without one
	PerHour         float64 // Used / Hours
}

// Recalc lists a customer's projects with work, ended and over budget
// first.
func Recalc(kimai *sources.KimaiDataset, customerID int64) []ProjectRecalc {
	var out []ProjectRecalc
	for _, p := range kimai.Projects {
		if p.CustomerID != customerID || p.UsedMinutes == 0 {
			continue
		}
		r := ProjectRecalc{Name: p.Name, End: p.End, Hours: round2(float64(p.UsedMinutes) / minutesPerHour), Used: round2(p.UsedMoney),
			Budget: p.Budget, BudgetH: round2(float64(p.TimeBudgetMin) / minutesPerHour)}
		switch {
		case r.Budget > 0:
			r.Share = r.Used / r.Budget
		case r.BudgetH > 0:
			r.Share = r.Hours / r.BudgetH
		}
		if r.Hours > 0 {
			r.PerHour = round2(r.Used / r.Hours)
		}
		out = append(out, r)
	}
	sort.SliceStable(out, func(a, b int) bool {
		if (out[a].Share > 1) != (out[b].Share > 1) {
			return out[a].Share > 1
		}
		return out[a].Hours > out[b].Hours
	})
	return out
}
