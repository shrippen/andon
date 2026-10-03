package metrics

// Freelance views over Kimai, Invoice Ninja and Sure:
//
//	HoursByDay      Kimai minutes per day (heatmap)
//	PaymentMorale   days from invoice to payment per client, recent vs. all
//	Cashflow        daily balance for the next days: bank + expected
//	                payments − recurring costs − tax deadlines

import (
	"sort"
	"time"

	"andon/internal/sources"
)

const (
	hoursDay     = 24
	moraleRecent = 3  // last invoices that make "recent"
	defaultTerms = 14 // payment days when a client has no history
)

// HoursByDay sums Kimai minutes per ISO day.
func HoursByDay(data *sources.KimaiDataset) map[string]int {
	out := map[string]int{}
	for _, s := range data.Timesheets {
		if d, ok := ParseDay(s.Begin); ok {
			out[d.Format(isoDay)] += s.Minutes
		}
	}
	return out
}

const isoDay = "2006-01-02"

// ── Payment morale ──

// MoraleRow is one client's payment behaviour.
type MoraleRow struct {
	ClientID   int64
	Client     string
	UsualDays  int // all paid invoices (mean or median, see Center)
	RecentDays int // the last moraleRecent
	Count      int
	Worse      bool // recent notably slower than usual
}

// PaymentMorale pairs each paid invoice with the client's next payment
// on or after its date (Invoice Ninja lists no per-invoice paid date).
func PaymentMorale(data *sources.NinjaDataset, slowerBy int, center Center) []MoraleRow {
	paid := map[int64][]time.Time{}
	for _, p := range data.Payments {
		if d, ok := ParseDay(p.Date); ok {
			paid[p.ClientID] = append(paid[p.ClientID], d)
		}
	}
	for _, list := range paid {
		sort.Slice(list, func(a, b int) bool { return list[a].Before(list[b]) })
	}

	type gap struct {
		at   time.Time
		days int
	}
	// Oldest invoice first; each payment settles one invoice.
	invoices := NinjaCounted(data)
	sort.SliceStable(invoices, func(a, b int) bool { return invoices[a].Date < invoices[b].Date })
	used := map[int64]int{}
	gaps := map[int64][]gap{}
	for _, i := range invoices {
		d, ok := ParseDay(i.Date)
		if !ok || i.Status != "paid" {
			continue
		}
		list := paid[i.ClientID]
		for k := used[i.ClientID]; k < len(list); k++ {
			if list[k].Before(d) {
				continue
			}
			gaps[i.ClientID] = append(gaps[i.ClientID], gap{d, int(list[k].Sub(d).Hours() / hoursDay)})
			used[i.ClientID] = k + 1
			break
		}
	}

	names := ninjaClientNames(data)
	var out []MoraleRow
	for id, list := range gaps {
		sort.Slice(list, func(a, b int) bool { return list[a].at.Before(list[b].at) })
		typical := func(part []gap) int {
			days := make([]int, len(part))
			for i, g := range part {
				days[i] = g.days
			}
			return center.days(days)
		}
		recent := list[max(0, len(list)-moraleRecent):]
		row := MoraleRow{ClientID: id, Client: names[id], UsualDays: typical(list), RecentDays: typical(recent), Count: len(list)}
		row.Worse = len(list) > moraleRecent && row.RecentDays-row.UsualDays >= slowerBy
		out = append(out, row)
	}
	sort.Slice(out, func(a, b int) bool { return out[a].RecentDays > out[b].RecentDays })
	return out
}

// ── Cashflow ──

// CashEvent is one expected movement.
type CashEvent struct {
	Day    time.Time
	Label  string
	Amount float64 // + in, − out
	Ref    string  // the invoice number of an expected payment, "" else
}

// CashPoint is the expected balance at the end of a day.
type CashPoint struct {
	Day     time.Time
	Balance float64
}

// CashLow is a projection's lowest balance and its first day.
func CashLow(points []CashPoint) (low float64, day time.Time) {
	if len(points) == 0 {
		return 0, time.Time{}
	}
	low, day = points[0].Balance, points[0].Day
	for _, p := range points {
		if p.Balance < low {
			low, day = p.Balance, p.Day
		}
	}
	return low, day
}

// CashInputs are the figures a cashflow needs; nil datasets are skipped.
type CashInputs struct {
	Ninja        *sources.NinjaDataset
	Sure         *sources.SureDataset
	FixedMonthly float64 // used when there is no Sure connection
	Tax          TaxSettings
	HasTax       bool
	VATInterval  string
	VATMethod    string
	Center       Center // typical payment delay per client
	DelayDays    int    // scenario: every open invoice paid this much later
	LateRef      string // scenario: this invoice (number) paid LateDays later
	LateDays     int
}

// Cashflow projects the balance for days ahead. Without Sure the start
// is 0: the curve then shows the change, not the balance.
func Cashflow(in CashInputs, today time.Time, days int) ([]CashPoint, []CashEvent) {
	end := today.AddDate(0, 0, days)
	var events []CashEvent
	add := func(day time.Time, label string, amount float64) {
		if day.Before(today) {
			day = today
		}
		if day.After(end) || amount == 0 {
			return
		}
		events = append(events, CashEvent{Day: day, Label: label, Amount: round2(amount)})
	}
	// addPaid is an expected payment of an invoice; the scenario moves one.
	addPaid := func(day time.Time, number, label string, amount float64) {
		if number == in.LateRef && number != "" {
			day = day.AddDate(0, 0, in.LateDays)
		}
		n := len(events)
		add(day, label, amount)
		if len(events) > n {
			events[n].Ref = number
		}
	}

	start := 0.0
	if in.Sure != nil {
		start = SureCash(in.Sure)
		for _, r := range in.Sure.Recurring {
			next, ok := ParseDay(r.Next)
			if r.Status != "active" || !r.Expense || !ok {
				continue
			}
			for d := next; !d.After(end); d = d.AddDate(0, 1, 0) {
				add(d, r.Name, -r.Amount)
			}
		}
	} else if in.FixedMonthly > 0 {
		for d := MonthStart(today).AddDate(0, 1, 0); !d.After(end); d = d.AddDate(0, 1, 0) {
			add(d, "fixed", -in.FixedMonthly)
		}
	}

	if in.Ninja != nil {
		delay := NinjaPaymentDays(in.Ninja, in.Center)
		for _, i := range NinjaOpenInvoices(in.Ninja, today) {
			issued, ok := ParseDay(i.Date)
			if !ok {
				continue
			}
			wait, known := delay[i.ClientID]
			if !known {
				wait = defaultTerms
			}
			addPaid(issued.AddDate(0, 0, wait+in.DelayDays), i.Number, i.Number+" "+i.Client, i.Balance)
		}
		for _, r := range in.Ninja.Recurring {
			if d, ok := ParseDay(r.NextSendDate); ok && r.Active {
				add(d.AddDate(0, 0, defaultTerms), r.Number, r.Amount)
			}
		}
	}

	if in.HasTax {
		for _, dl := range UpcomingDeadlines(in.Tax, today, days) {
			switch dl.Kind {
			case "prepayment":
				if dl.Amount != nil {
					add(dl.Due, "prepayment", -*dl.Amount)
				}
			case "vat_return":
				if in.Ninja != nil {
					liability := NinjaOutputVAT(in.Ninja, dl.PeriodStart, dl.PeriodEnd, in.VATMethod) - NinjaInputVAT(in.Ninja, dl.PeriodStart, dl.PeriodEnd)
					add(dl.Due, "vat "+dl.Period, -liability)
				}
			}
		}
	}

	sort.SliceStable(events, func(a, b int) bool { return events[a].Day.Before(events[b].Day) })
	points := make([]CashPoint, 0, days+1)
	balance, next := start, 0
	for d := today; !d.After(end); d = d.AddDate(0, 0, 1) {
		for next < len(events) && !events[next].Day.After(d) {
			balance += events[next].Amount
			next++
		}
		points = append(points, CashPoint{Day: d, Balance: round2(balance)})
	}
	return points, events
}
