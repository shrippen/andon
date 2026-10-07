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
}

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

	for _, inv := range ninja.Invoices {
		card, ok := byClient[inv.ClientID]
		if ok && isCounted(inv.Status) && span.HasDay(inv.Date) {
			card.Revenue += inv.Net
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
