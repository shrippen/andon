// Package year prepares the tax year (EÜR) of one Verbund: what came
// in, the expenses by EÜR line, the depreciation of the assets and the
// days worked at home, with the gaps that still need work and the
// package for the tax advisor (billing.Export). It only reads.
//
//	Invoice Ninja ─ payments, VAT, expenses ─┐
//	Snipe-IT ───── depreciation ─────────────┼─► Page ─► /euer, /receipts …
//	Kimai, Dawarich ─ home office days ──────┘        └► steuer-<year>.zip
package year

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/svcdata"
	"andon/internal/services/workbench"
	"andon/internal/sources"
)

// ErrNoNinja means the user reaches no Invoice Ninja.
var ErrNoNinja = errors.New("year.err_no_ninja")

var need = workbench.Need{Required: []enums.ServiceType{enums.ServiceInvoiceNinja},
	Optional: []enums.ServiceType{enums.ServiceKimai, enums.ServiceDawarich, enums.ServiceSnipeIT}}

// yearsShown is how many years the page offers, this one first.
const yearsShown = 3

const prefKey = "year"

type state struct {
	Scope string `json:"scope,omitempty"`
}

// Page is the year of one Verbund.
type Page struct {
	Scopes   []workbench.Scope
	Scope    string
	Choose   bool
	SpaceID  int64
	LinkID   int64
	Year     int
	Years    []int
	Currency string

	// Income: payments received (gross) and the VAT in them, input VAT.
	Paid, OutputVAT, InputVAT float64

	Lines    []metrics.EuerLine // expenses by EÜR line
	Unlined  int                // expenses without line
	EuerSlot int                // 0: no field chosen yet
	Expenses float64            // net, all lines

	Snipe    bool // the Verbund has Snipe-IT
	Afa      []metrics.AfaRow
	AfaTotal float64
	NoRule   int // assets without depreciation rule

	Review metrics.YearReview // the year at a glance

	Kimai     bool
	HomeDays  int
	Estimated bool // no Dawarich: rides unknown
	Allowance float64
}

// Open builds the page for year (0: last year).
func Open(ctx context.Context, d *sql.DB, who *access.Principal, scope string, year int, now time.Time) (Page, error) {
	p := Page{Year: year}
	if p.Year == 0 {
		p.Year = now.Year() - 1
	}
	for i := range yearsShown {
		p.Years = append(p.Years, now.Year()-i)
	}

	scopes, err := workbench.Scopes(d, who, need)
	p.Scopes = scopes
	if err != nil {
		return p, err
	}
	if len(scopes) == 0 {
		return p, ErrNoNinja
	}
	st, err := workbench.LoadState[state](d, who, prefKey)
	if err != nil {
		return p, err
	}
	if scope == "" {
		scope = st.Scope
	}
	s, choose := workbench.Pick(scopes, scope)
	p.Scope, p.Choose, p.LinkID = s.Key, choose, s.Link

	ninjaConn := s.Conn(enums.ServiceInvoiceNinja)
	p.SpaceID = ninjaConn.SpaceID
	data := fetchAll(ctx, d, who, s)
	ninja, _ := data[enums.ServiceInvoiceNinja].(*sources.NinjaDataset)
	if ninja == nil {
		return p, ErrNoNinja
	}
	settings, err := spaceSettings(d, p.SpaceID)
	if err != nil {
		return p, err
	}

	kimai, _ := data[enums.ServiceKimai].(*sources.KimaiDataset)
	p.Review = metrics.ReviewYear(kimai, ninja, p.Year)
	p.income(ninja, metrics.TaxVATMethod(settings))
	p.expenses(ninja, sources.CategoriesOf(ninjaConn.Options).EuerSlot)
	if snipe, ok := data[enums.ServiceSnipeIT].(*sources.SnipeDataset); ok {
		p.depreciation(snipe)
	}
	if kimai != nil {
		travel, whole := travelOf(data, kimai, s, settings, now, p.Year)
		p.homeOffice(kimai, travel)
		p.Estimated = p.Estimated || !whole
	}

	err = workbench.SaveState(d, who, prefKey, func(st *state) { st.Scope = s.Key })
	return p, err
}

// fetchAll reads the Verbund's datasets, as last stored: the page shows
// a year, which the latest background run covers.
func fetchAll(ctx context.Context, d *sql.DB, who *access.Principal, s workbench.Scope) map[enums.ServiceType]any {
	out := map[enums.ServiceType]any{}
	holder := model.UserHolder(who.UserID)
	for _, service := range append(need.Required, need.Optional...) {
		c := s.Conn(service)
		if c == nil {
			continue
		}
		res, err := svcdata.Get(ctx, d, sources.DataKey(service), nil, c, holder, svcdata.Stored)
		if err == nil && res.Pending {
			res, err = svcdata.Get(ctx, d, sources.DataKey(service), nil, c, holder, svcdata.Cached)
		}
		if err == nil && res.Data != nil {
			out[service] = res.Data
		}
	}
	return out
}

func spaceSettings(d *sql.DB, spaceID int64) (map[string]any, error) {
	var settings map[string]any
	err := db.WithRead(d, func(tx *sql.Tx) error {
		sp, err := content.Space(tx, spaceID)
		if err == nil && sp != nil {
			settings = sp.Settings
		}
		return err
	})
	return settings, err
}

func (p *Page) span() (time.Time, time.Time) {
	start := time.Date(p.Year, 1, 1, 0, 0, 0, 0, time.UTC)
	return start, start.AddDate(1, 0, -1)
}

// income: what came in during the year (EÜR counts payments) and the
// VAT in and out.
func (p *Page) income(ninja *sources.NinjaDataset, method string) {
	start, end := p.span()
	p.Currency = ninja.Currency
	for _, pay := range ninja.Payments {
		if d, ok := metrics.ParseDay(pay.Date); ok && !d.Before(start) && !d.After(end) {
			p.Paid += pay.Amount
		}
	}
	p.OutputVAT, p.InputVAT = metrics.NinjaOutputVAT(ninja, start, end, method), metrics.NinjaInputVAT(ninja, start, end)
}

func (p *Page) expenses(ninja *sources.NinjaDataset, slot int) {
	p.EuerSlot = slot
	p.Lines = metrics.EuerLines(ninja, slot, p.Year)
	for _, l := range p.Lines {
		p.Expenses += l.Net
		if l.Line == "" {
			p.Unlined = l.Expenses
		}
	}
}

func (p *Page) depreciation(snipe *sources.SnipeDataset) {
	p.Snipe = true
	p.Afa = metrics.Depreciation(snipe, p.Year)
	for _, r := range p.Afa {
		p.AfaTotal += r.Year
		if r.Months == 0 {
			p.NoRule++
		}
	}
}

func (p *Page) homeOffice(kimai *sources.KimaiDataset, travel *metrics.Travel) {
	p.Kimai = true
	days, estimated := metrics.HomeOffice(kimai, travel, p.Year)
	p.HomeDays, p.Estimated = len(days), estimated
	p.Allowance = metrics.HomeOfficeAllowance(p.HomeDays, metrics.HomeOfficeRate, metrics.HomeOfficeMaxDays)
}

// travelOf classifies the Verbund's rides, nil without Dawarich; whole
// says whether its tracks reach back to the year's start (the stored
// dataset covers recent weeks, the package reads the whole year).
func travelOf(data map[enums.ServiceType]any, kimai *sources.KimaiDataset, s workbench.Scope, settings map[string]any, now time.Time, year int) (*metrics.Travel, bool) {
	geo, ok := data[enums.ServiceDawarich].(*sources.DawarichDataset)
	if !ok {
		return nil, false
	}
	t := metrics.TravelOf(geo, kimai, s.Conn(enums.ServiceDawarich).Options, metrics.TravelSettingsOf(settings), now)
	from, err := time.Parse(time.RFC3339, geo.TracksFrom)
	return &t, err == nil && !from.After(time.Date(year, 1, 1, 0, 0, 0, 0, time.UTC))
}
