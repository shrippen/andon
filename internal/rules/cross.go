// Package rules: cross-service rules of one space (and one credential owner).
//
//	dawarich <-> kimai      client visit without booking, booking "on site" without visit
//	dawarich                tracking stopped (rides: travel.go)
//	snipeit <-> invoiceninja purchase without expense
package rules

import (
	"fmt"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const (
	reportDays   = 7
	lookbackDays = 30
)

func areaMapping(env Env) map[string]metrics.AreaMapping {
	geo, _ := env.Datasets[string(enums.ServiceDawarich)].(*sources.DawarichDataset)
	kimai, _ := env.Datasets[string(enums.ServiceKimai)].(*sources.KimaiDataset)
	return metrics.AreaMap(geo, kimai, env.Options[string(enums.ServiceDawarich)])
}

func booked(kimai *sources.KimaiDataset) map[[2]any]bool {
	out := map[[2]any]bool{}
	for _, s := range kimai.Timesheets {
		if d, ok := metrics.ParseDay(s.Begin); ok {
			out[[2]any{d, s.CustomerID}] = true
		}
	}
	return out
}

func kimaiNames(kimai *sources.KimaiDataset) map[int64]string {
	return metrics.KimaiCustomerNames(kimai)
}

func lastMonth(today time.Time) (time.Time, time.Time) {
	start := metrics.AddMonths(today, -1)
	return start, metrics.MonthStart(today).AddDate(0, 0, -1)
}

func init() {
	Register("geo.visit_without_time", Cross, map[string]any{"min_minutes": 120.0}, visitWithoutTime)

	Register("geo.time_without_visit", Cross, map[string]any{"keywords": []any{"vor ort", "on-site", "onsite"}}, timeWithoutVisit)

	Register("geo.no_data", string(enums.ServiceDawarich), map[string]any{"hours": 24.0}, on(geoNoData))

	Register("snipe.expense_missing", Cross, map[string]any{"days": 365.0, "tolerance": 0.05, "date_window": 14.0}, expenseMissing)
}

func visitWithoutTime(_ any, cfg map[string]any, env Env) []Finding {
	kimai, ok1 := env.Datasets[string(enums.ServiceKimai)].(*sources.KimaiDataset)
	geo, ok2 := env.Datasets[string(enums.ServiceDawarich)].(*sources.DawarichDataset)
	if !ok1 || !ok2 {
		return nil
	}
	bk, names := booked(kimai), kimaiNames(kimai)
	since := env.Today.AddDate(0, 0, -lookbackDays)

	var found []Finding
	for _, v := range metrics.ClientVisits(geo, areaMapping(env)) {
		if v.Day.Before(since) || !v.Day.Before(env.Today) {
			continue
		}
		if v.Minutes < cfgInt(cfg, "min_minutes") {
			continue
		}
		if bk[[2]any{v.Day, v.CustomerID}] {
			continue
		}
		name := names[v.CustomerID]
		if name == "" {
			name = "?"
		}
		found = append(found, Finding{
			Fingerprint: fmt.Sprintf("visit:%s:%d", v.Day.Format("2006-01-02"), v.CustomerID),
			Rule:        "geo.visit_without_time", Severity: enums.SeverityWarn, Message: "geo.visit_without_time",
			Params: map[string]any{
				"customer": name, "day": Day(v.Day), "hours": Num(float64(v.Minutes)/60, 1),
			},
			ActionURL: strings.TrimRight(kimai.URL, "/") + "/timesheet/", ActionLabel: "open_in_kimai",
			Sources: []string{string(enums.ServiceDawarich), string(enums.ServiceKimai)},
		})
	}
	return found
}

func timeWithoutVisit(_ any, cfg map[string]any, env Env) []Finding {
	kimai, ok1 := env.Datasets[string(enums.ServiceKimai)].(*sources.KimaiDataset)
	geo, ok2 := env.Datasets[string(enums.ServiceDawarich)].(*sources.DawarichDataset)
	if !ok1 || !ok2 {
		return nil
	}
	mapping := areaMapping(env)
	mapped := map[int64]bool{}
	visited := map[[2]any]bool{}
	for _, v := range metrics.ClientVisits(geo, mapping) {
		visited[[2]any{v.Day, v.CustomerID}] = true
	}
	travel, _ := travelOf(env)
	for _, r := range travel.Rides {
		if r.Class == metrics.ClassBusiness && r.CustomerID != 0 {
			visited[[2]any{r.Day(), r.CustomerID}] = true
			mapped[r.CustomerID] = true
		}
	}
	for _, m := range mapping {
		if m.CustomerID != 0 {
			mapped[m.CustomerID] = true
		}
	}
	names := kimaiNames(kimai)
	since := env.Today.AddDate(0, 0, -lookbackDays)
	words := stringsSlice(cfg["keywords"])

	var found []Finding
	for _, s := range kimai.Timesheets {
		when, ok := metrics.ParseDay(s.Begin)
		if !ok || when.Before(since) || !mapped[s.CustomerID] {
			continue
		}
		if !containsAny(strings.ToLower(s.Activity), words) {
			continue
		}
		if visited[[2]any{when, s.CustomerID}] {
			continue
		}
		name := names[s.CustomerID]
		if name == "" {
			name = "?"
		}
		found = append(found, Finding{
			Fingerprint: fmt.Sprintf("novisit:%d", s.ID), Severity: enums.SeverityInfo, Message: "geo.time_without_visit",
			Params:  map[string]any{"customer": name, "day": Day(when)},
			Sources: []string{string(enums.ServiceKimai), string(enums.ServiceDawarich)},
		})
	}
	return found
}

func geoNoData(data *sources.DawarichDataset, cfg map[string]any, env Env) []Finding {
	last, ok := metrics.ParseTime(data.LastPoint)
	if !ok {
		return nil
	}
	silent := time.Now().UTC().Sub(last).Hours()
	if silent < cfgFloat(cfg, "hours") {
		return nil
	}
	return []Finding{{
		Fingerprint: "nodata", Severity: enums.SeverityWarn, Message: "geo.no_data",
		Params:    map[string]any{"hours": Num(silent, 0)},
		ActionURL: strings.TrimRight(data.URL, "/") + "/map", ActionLabel: "open_in_dawarich",
		Sources: []string{string(enums.ServiceDawarich)},
	}}
}

func expenseMissing(_ any, cfg map[string]any, env Env) []Finding {
	assets, ok1 := env.Datasets[string(enums.ServiceSnipeIT)].(*sources.SnipeDataset)
	ninja, ok2 := env.Datasets[string(enums.ServiceInvoiceNinja)].(*sources.NinjaDataset)
	if !ok1 || !ok2 {
		return nil
	}
	var found []Finding
	for _, asset := range RecentPurchases(assets, env.Today, cfgInt(cfg, "days")) {
		bought, ok := metrics.ParseDay(asset.PurchaseDate)
		if !ok || bought.Year() != env.Today.Year() {
			continue
		}
		cost := asset.PurchaseCost
		tolerance := cost * cfgFloat(cfg, "tolerance")
		window := cfgInt(cfg, "date_window")
		matched := false
		for _, e := range ninja.Expenses {
			near := absF(e.Amount-cost) <= tolerance || absF(e.Amount-e.Tax-cost) <= tolerance
			if !near {
				continue
			}
			d, ok := metrics.ParseDay(e.Date)
			if ok && absDays(d, bought) <= window {
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		found = append(found, Finding{
			Fingerprint: fmt.Sprintf("expense:%d", asset.ID), Severity: enums.SeverityInfo, Message: "snipe.expense_missing",
			Params:  map[string]any{"asset": asset.Name, "amount": Money(cost, ""), "day": Day(bought)},
			Sources: []string{string(enums.ServiceSnipeIT), string(enums.ServiceInvoiceNinja)},
		})
	}
	return found
}

func stringsSlice(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, s := range list {
		if str, ok := s.(string); ok {
			out = append(out, strings.ToLower(str))
		}
	}
	return out
}

func containsAny(haystack string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(haystack, n) {
			return true
		}
	}
	return false
}

func absF(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func absDays(a, b time.Time) int {
	d := int(a.Sub(b).Hours() / 24)
	if d < 0 {
		return -d
	}
	return d
}
