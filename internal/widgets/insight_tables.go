package widgets

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// ── Tables ──

// Col is one table column: its header key and how to format each row's
// value at the same index.
type Col struct{ Label, Format string }

// numericFormats are the column formats set right-aligned.
var numericFormats = map[string]bool{"money": true, "hours": true, "km": true, "daycount": true, "late": true}

// Numeric tells whether the column holds numbers (right-aligned).
func (c Col) Numeric() bool { return numericFormats[c.Format] }

// Row is one table row; Values line up with the widget's Cols.
type Row struct{ Values []any }

func colsFor(kind TableKind) []Col {
	switch kind {
	case TableOpenInvoices:
		return []Col{{"number", "text"}, {"client", "text"}, {"due", "day"}, {"late", "late"}, {"amount", "money"}}
	case TableClientShares:
		return []Col{{"client", "text"}, {"amount", "money"}, {"share", "pct"}}
	case TableUnbilled:
		return []Col{{"customer", "text"}, {"hours", "hours"}, {"amount", "money"}, {"oldest", "day"}}
	case TableBudgets:
		return []Col{{"project", "text"}, {"used", "bar"}}
	case TableAssetDates:
		return []Col{{"name", "text"}, {"kind", "upcoming"}, {"due", "day"}}
	case TableTrips:
		return []Col{{"day", "day"}, {"from", "text"}, {"to", "text"}, {"class", "rideclass"}, {"reason", "ridereason"}, {"km", "km"}, {"duration", "hours"}}
	case TableTripCustomers:
		return []Col{{"customer", "text"}, {"km", "km"}, {"hours", "hours"}, {"amount", "money"}}
	case TableDestinations:
		return []Col{{"place", "text"}, {"rides", "text"}, {"km", "km"}, {"last", "day"}}
	case TableRates:
		return []Col{{"customer", "text"}, {"hours", "hours"}, {"amount", "money"}, {"rate", "money"}}
	case TableAppUsage:
		return []Col{{"app", "text"}, {"logins", "text"}, {"users", "text"}}
	case TableMorale:
		return []Col{{"client", "text"}, {"avg_days", "daycount"}, {"recent_days", "daycount"}, {"invoices", "text"}}
	}
	if cols := crossCols(kind); cols != nil {
		return cols
	}
	return homelabCols(kind)
}

func tableRows(kind TableKind, results map[string]any, ctx ViewCtx) ([]Row, bool) {
	data, ok := results["data"]
	if !ok || data == nil {
		return nil, false
	}
	if rows, ok := crossRows(kind, data, results, ctx); ok {
		return rows, true
	}
	if rows, ok := homelabRows(kind, data, results, ctx); ok {
		return rows, true
	}
	today := todayOf(ctx)
	service := enums.ServiceType(ctx.Service)

	switch {
	case kind == TableOpenInvoices && service == enums.ServiceInvoiceNinja:
		var rows []Row
		for _, i := range metrics.NinjaOpenInvoices(data.(*sources.NinjaDataset), today) {
			rows = append(rows, Row{[]any{i.Number, i.Client, i.DueDate, i.OverdueDays, i.Balance}})
		}
		return rows, true

	case kind == TableClientShares && service == enums.ServiceInvoiceNinja:
		var rows []Row
		for _, s := range metrics.NinjaShares(data.(*sources.NinjaDataset), today) {
			rows = append(rows, Row{[]any{s.Client, s.Net, s.Share}})
		}
		return rows, true

	case kind == TableUnbilled && service == enums.ServiceKimai:
		var rows []Row
		for _, g := range metrics.KimaiUnbilled(data.(*sources.KimaiDataset), today) {
			rows = append(rows, Row{[]any{g.Customer, float64(g.Minutes) / minutesPerHourInsight, g.Amount, g.Oldest}})
		}
		return rows, true

	case kind == TableBudgets && service == enums.ServiceKimai:
		var rows []Row
		for _, b := range kimaiBudgets(data.(*sources.KimaiDataset), today) {
			rows = append(rows, Row{[]any{b.Name, b.Pct}})
		}
		return rows, true

	case kind == TableAssetDates && service == enums.ServiceSnipeIT:
		var rows []Row
		for _, i := range metrics.SnipeUpcomingDates(data.(*sources.SnipeDataset), today, 0) {
			rows = append(rows, Row{[]any{i.Name, i.Kind, i.Date}})
		}
		return rows, true

	case kind == TableRates && service == enums.ServiceInvoiceNinja:
		kimai, ok := results[peerKimai].(*sources.KimaiDataset)
		if !ok {
			return nil, false
		}
		rates, _ := metrics.EffectiveRates(kimai, data.(*sources.NinjaDataset), today)
		var rows []Row
		for _, r := range rates {
			rows = append(rows, Row{[]any{r.Customer, r.Hours, r.Net, r.Rate}})
		}
		return rows, true

	case kind == TableMorale && service == enums.ServiceInvoiceNinja:
		var rows []Row
		for _, m := range metrics.PaymentMorale(data.(*sources.NinjaDataset), moraleSlower, metrics.CenterOf(ctx.Settings)) {
			rows = append(rows, Row{[]any{m.Client, m.UsualDays, m.RecentDays, m.Count}})
		}
		return rows, true

	case kind == TableAppUsage && service == enums.ServiceAuthentik:
		var rows []Row
		for _, a := range data.(*sources.AuthentikDataset).Apps {
			rows = append(rows, Row{[]any{a.Name, a.Events, a.Users}})
		}
		return rows, true

	case service == enums.ServiceDawarich:
		return tripRows(kind, data.(*sources.DawarichDataset), results, ctx, today)
	}
	return nil, false
}

// budgetRow is one Kimai project's budget usage.
type budgetRow struct {
	ID      int64
	Name    string
	Pct     float64
	Monthly bool // a monthly time budget: the month is its period
}

// kimaiBudgets: each project's budget use as kimai.budget_burn counts it.
func kimaiBudgets(data *sources.KimaiDataset, today time.Time) []budgetRow {
	var rows []budgetRow
	for _, p := range data.Projects {
		if share, ok := metrics.BudgetUse(p, data, today); ok {
			rows = append(rows, budgetRow{ID: p.ID, Name: p.Name, Pct: share, Monthly: p.BudgetType == "month"})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Pct > rows[j].Pct })
	return rows
}

func tableView(cfg TableConfig, results map[string]any, ctx ViewCtx) map[string]any {
	rows, ok := tableRows(cfg.Table, results, ctx)
	if !ok {
		if _, hasData := results["data"]; hasData {
			return map[string]any{"Unsupported": true}
		}
		return map[string]any{}
	}
	cols := colsFor(cfg.Table)
	sortRows(rows, cols, cfg.Sort)
	var sum Row
	if cfg.SumRow {
		sum = sumRow(rows, cols)
	}
	cols, rows, sum = hideCols(cols, rows, sum, cfg.HideCols)
	total := len(rows)
	if total > cfg.Limit {
		rows = rows[:cfg.Limit]
	}
	out := map[string]any{"Cols": cols, "Rows": rows, "Total": total, "More": total - len(rows)}
	if cfg.SumRow {
		out["Sum"] = sum
	}
	return out
}

// Table sort orders.
const (
	sortAsIs       = "as_is"
	sortAmountDesc = "amount_desc"
	sortAmountAsc  = "amount_asc"
	sortName       = "name"
	sortDate       = "date"
)

// sortRows orders rows by the first column of the kind the order needs.
func sortRows(rows []Row, cols []Col, order string) {
	find := func(formats ...string) int {
		for i, c := range cols {
			if slices.Contains(formats, c.Format) {
				return i
			}
		}
		return -1
	}
	switch order {
	case sortAmountDesc, sortAmountAsc:
		i := find("money", "hours", "km", "pct")
		if i < 0 {
			return
		}
		sort.SliceStable(rows, func(a, b int) bool {
			x, y := asFloat(rows[a].Values[i]), asFloat(rows[b].Values[i])
			if order == sortAmountAsc {
				return x < y
			}
			return x > y
		})
	case sortName, sortDate:
		i := find("text")
		if order == sortDate {
			i = find("day")
		}
		if i < 0 {
			return
		}
		sort.SliceStable(rows, func(a, b int) bool {
			return strings.ToLower(fmt.Sprint(rows[a].Values[i])) < strings.ToLower(fmt.Sprint(rows[b].Values[i]))
		})
	}
}

// summed are the column formats a sum row adds up.
var summed = map[string]bool{"money": true, "hours": true, "km": true}

// sumRow adds up the summable columns of all rows ("" elsewhere).
func sumRow(rows []Row, cols []Col) Row {
	out := Row{Values: make([]any, len(cols))}
	for i, c := range cols {
		if !summed[c.Format] {
			out.Values[i] = ""
			continue
		}
		total := 0.0
		for _, r := range rows {
			total += asFloat(r.Values[i])
		}
		out.Values[i] = total
	}
	return out
}

// hideCols drops the columns named in hide, by key or by their name in
// German or English.
func hideCols(cols []Col, rows []Row, sum Row, hide []string) ([]Col, []Row, Row) {
	if len(hide) == 0 {
		return cols, rows, sum
	}
	var keep []int
	var kept []Col
	for i, c := range cols {
		names := []string{c.Label, strings.ToLower(i18n.T("col."+c.Label, enums.LocaleDE, nil)), strings.ToLower(i18n.T("col."+c.Label, enums.LocaleEN, nil))}
		if slices.ContainsFunc(names, func(n string) bool { return slices.Contains(hide, n) }) {
			continue
		}
		keep = append(keep, i)
		kept = append(kept, c)
	}
	pick := func(r Row) Row {
		if r.Values == nil {
			return r
		}
		out := Row{Values: make([]any, len(keep))}
		for j, i := range keep {
			out.Values[j] = r.Values[i]
		}
		return out
	}
	for i := range rows {
		rows[i] = pick(rows[i])
	}
	return kept, rows, pick(sum)
}

// tripRows are the Dawarich tables of the last month: the rides, business
// per customer, destinations.
func tripRows(kind TableKind, geo *sources.DawarichDataset, results map[string]any, ctx ViewCtx, today time.Time) ([]Row, bool) {
	travel := travelOf(geo, ctx, results)
	rides := travel.Between(metrics.AddMonths(today, -1), today)
	var rows []Row
	switch kind {
	case TableTrips:
		for i := len(rides) - 1; i >= 0; i-- {
			r := rides[i]
			rows = append(rows, Row{[]any{r.Day().Format(time.DateOnly), siteLabel(r.From), siteLabel(r.To), string(r.Class), string(r.Reason), r.KM, r.Minutes() / minutesPerHourInsight}})
		}
	case TableTripCustomers:
		kimai, _ := results[peerKimai].(*sources.KimaiDataset)
		names := map[int64]string{}
		if kimai != nil {
			names = metrics.KimaiCustomerNames(kimai)
		}
		rate := metrics.TravelSettingsOf(ctx.Settings).KMRate
		for id, s := range metrics.ByCustomer(rides) {
			name := names[id]
			if name == "" {
				name = "?"
			}
			rows = append(rows, Row{[]any{name, s.KM, s.Minutes / minutesPerHourInsight, s.PayKM * rate}})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Values[1].(float64) > rows[j].Values[1].(float64) })
	case TableDestinations:
		for _, d := range metrics.Destinations(rides) {
			name := "?"
			if d.Site != nil {
				name = d.Site.Name
			}
			rows = append(rows, Row{[]any{name, d.Rides, d.KM, d.Last.In(time.Local).Format(time.DateOnly)}})
		}
	default:
		return nil, false
	}
	return rows, true
}
