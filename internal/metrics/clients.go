package metrics

import (
	"sort"
	"time"

	"andon/internal/sources"
)

// ClientCard is one Kimai customer with its Invoice Ninja client (joined
// by name): the numbers of the customer page.
type ClientCard struct {
	CustomerID  int64
	Name        string
	Matched     bool // an Invoice Ninja client of the same name exists
	HoursYear   float64
	HoursMonth  float64
	Unbilled    float64 // billable, not exported work
	Open        float64 // balance of open invoices
	Overdue     float64
	RevenueYTD  float64 // net of this year's counted invoices
	PaymentDays int     // typical days to payment, 0 without history
	LastWork    string  // day of the newest time entry, "" if none
	Invoices    []NinjaOpenInvoice
	Projects    []ClientProject
}

// ClientProject is a project's hours this year.
type ClientProject struct {
	Name  string
	Hours float64
}

// ClientCards builds a card per Kimai customer, most hours this year
// first. ninja may be nil.
func ClientCards(kimai *sources.KimaiDataset, ninja *sources.NinjaDataset, today time.Time, center Center) []ClientCard {
	year, month := today.Format("2006"), today.Format("2006-01")
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

	for _, s := range kimai.Timesheets {
		card, ok := cards[s.CustomerID]
		if !ok || len(s.Begin) < len("2006-01-02") {
			continue
		}
		day := s.Begin[:len("2006-01-02")]
		hours := float64(s.Minutes) / minutesPerHour
		if day > card.LastWork {
			card.LastWork = day
		}
		if s.Billable && !s.Exported && s.End != "" {
			card.Unbilled += s.Rate
		}
		if day[:4] != year {
			continue
		}
		card.HoursYear += hours
		if day[:7] == month {
			card.HoursMonth += hours
		}
		if p, ok := projects[s.ProjectID]; ok {
			projectHours[s.CustomerID][p.Name] += hours
		}
	}

	if ninja != nil {
		joinNinja(cards, ninja, today, center)
	}

	out := make([]ClientCard, 0, len(cards))
	for id, card := range cards {
		for name, h := range projectHours[id] {
			card.Projects = append(card.Projects, ClientProject{Name: name, Hours: round2(h)})
		}
		sort.Slice(card.Projects, func(a, b int) bool { return card.Projects[a].Hours > card.Projects[b].Hours })
		card.HoursYear, card.HoursMonth = round2(card.HoursYear), round2(card.HoursMonth)
		out = append(out, *card)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].HoursYear != out[b].HoursYear {
			return out[a].HoursYear > out[b].HoursYear
		}
		return out[a].Name < out[b].Name
	})
	return out
}

// joinNinja adds invoices, revenue and payment days of the client with
// the same name as each customer.
func joinNinja(cards map[int64]*ClientCard, ninja *sources.NinjaDataset, today time.Time, center Center) {
	clientOf := map[string]int64{}
	for _, c := range ninja.Clients {
		clientOf[nameKey(c.Name)] = c.ID
	}
	byClient := map[int64]*ClientCard{}
	for _, card := range cards {
		if id, ok := clientOf[nameKey(card.Name)]; ok {
			card.Matched = true
			byClient[id] = card
		}
	}

	year := today.Format("2006")
	for _, inv := range ninja.Invoices {
		card, ok := byClient[inv.ClientID]
		if ok && isCounted(inv.Status) && len(inv.Date) >= 4 && inv.Date[:4] == year {
			card.RevenueYTD += inv.Net
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
