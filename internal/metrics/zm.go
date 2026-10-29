package metrics

// The Zusammenfassende Meldung (§ 18a UStG): services to businesses in
// other EU countries, reported to the BZSt per quarter and VAT ID, by
// the 25th after the quarter (no Dauerfristverlängerung). The same
// invoices as Kz 21, by invoice date (when the service was rendered),
// whatever the VAT method:
//
//	Q3 2026, due 25.10.
//	AT  U12345678    400  Studio Graz, Studio Wien
//	FR  12345678901  750  Atelier Lyon
//	without VAT ID: Agentur Gent

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"

	"andon/internal/sources"
)

// zmDueDay is the day of the month after the quarter the report is due.
const zmDueDay = 25

// ZM is one quarter's report.
type ZM struct {
	Key   string // "2026-Q3", as VATSpan keys
	Start time.Time
	End   time.Time
	Due   time.Time
	Lines []ZMLine
	NoID  []string // clients with such invoices but no VAT ID in Invoice Ninja
}

// ZMLine is one VAT ID's sum: country code and number as the form asks
// for them, the base in whole euros.
type ZMLine struct {
	Country string
	ID      string
	Amount  float64
	Clients string // the clients carrying the ID, for the reader
}

// ZMOf is the report of the quarter holding day.
func ZMOf(n *sources.NinjaDataset, day time.Time) ZM {
	start := time.Date(day.Year(), time.Month((int(day.Month())-1)/quarterMonths*quarterMonths+1), 1, 0, 0, 0, 0, time.UTC)
	end := AddMonths(start, quarterMonths).AddDate(0, 0, -1)
	due := AddMonths(start, quarterMonths).AddDate(0, 0, zmDueDay-1)
	zm := ZM{Key: fmt.Sprintf("%d-Q%d", start.Year(), (int(start.Month())-1)/quarterMonths+1), Start: start, End: end, Due: due}
	if n == nil {
		return zm
	}

	clients := map[int64]sources.NinjaClient{}
	for _, c := range n.Clients {
		clients[c.ID] = c
	}

	// Sum per VAT ID; a client without one is only named.
	sums := map[string]float64{}
	names := map[string][]string{}
	for _, inv := range NinjaCounted(n) {
		d, ok := ParseDay(inv.Date)
		c := clients[inv.ClientID]
		if !ok || d.Before(start) || d.After(end) || invoiceRate(inv) != 0 || !euCountries[c.CountryID] || c.CountryID == n.HomeCountryID {
			continue
		}
		id := vatID(c.VATNumber)
		if id == "" {
			if !slices.Contains(zm.NoID, c.Name) {
				zm.NoID = append(zm.NoID, c.Name)
			}
			continue
		}
		sums[id] += inv.Net
		if !slices.Contains(names[id], c.Name) {
			names[id] = append(names[id], c.Name)
		}
	}

	for id, sum := range sums {
		slices.Sort(names[id])
		zm.Lines = append(zm.Lines, ZMLine{Country: id[:2], ID: id[2:], Amount: math.Trunc(sum), Clients: strings.Join(names[id], ", ")})
	}
	slices.SortFunc(zm.Lines, func(a, b ZMLine) int { return strings.Compare(a.Country+a.ID, b.Country+b.ID) })
	slices.Sort(zm.NoID)
	return zm
}

// vatID normalizes a VAT ID as typed ("fr 12 345678901" → "FR12345678901");
// "" when it lacks the country prefix or a number.
func vatID(raw string) string {
	id := strings.ToUpper(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || r == '.' || r == '-' {
			return -1
		}
		return r
	}, raw))
	if len(id) < 3 || !unicode.IsLetter(rune(id[0])) || !unicode.IsLetter(rune(id[1])) {
		return ""
	}
	return id
}
