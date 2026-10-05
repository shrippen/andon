package rules

// Rides from Dawarich tracks, classified business / commute / private
// (metrics.Classify):
//
//	geo.travel_costs        business km of last month × km rate
//	geo.per_diem            meal allowance days of last month
//	geo.travel_unbilled     a customer's business km of last month in no invoice
//	geo.unplaced            places rides often end at without a site
//	geo.plugin_missing      business rides the Kimai mileage plugin lacks
//	geo.car_private_share   business car driven mostly privately
//	geo.tracks_missing      no tracks: distances are estimates

import (
	"fmt"
	"math"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const (
	dawarichSvc = string(enums.ServiceDawarich)
	// placesURL opens the places tab of the space's Dawarich connection.
	placesURL = "/travel/places"
)

// travelOf classifies the space's Dawarich rides; false without Dawarich.
func travelOf(env Env) (metrics.Travel, bool) {
	geo, ok := env.Datasets[dawarichSvc].(*sources.DawarichDataset)
	if !ok {
		return metrics.Travel{}, false
	}
	kimai, _ := env.Datasets[kimaiSvc].(*sources.KimaiDataset)
	set := metrics.TravelSettingsOf(env.Settings)
	return metrics.TravelOf(geo, kimai, env.Options[dawarichSvc], set, time.Now()), true
}

func init() {
	Register("geo.travel_costs", Cross, map[string]any{}, travelCosts)

	Register("geo.per_diem", Cross, map[string]any{}, perDiem)

	Register("geo.travel_unbilled", Cross, map[string]any{"words": []any{"fahrt", "anfahrt", "reise", "km", "travel", "mileage"}}, travelUnbilled)

	Register("geo.unplaced", Cross, map[string]any{"rides": 3.0}, unplaced)

	Register("geo.plugin_missing", Cross, map[string]any{}, pluginMissing)

	Register("geo.car_private_share", Cross, map[string]any{"private_share": metrics.CarShareLimit, "min_km": 500.0}, carPrivateShare)

	Register("geo.tracks_missing", dawarichSvc, map[string]any{}, on(tracksMissing))
}

// lastMonthRides is the rides of last month, reported in the first days
// of this one.
func lastMonthRides(env Env) (metrics.Travel, []metrics.ClassedRide, time.Time, bool) {
	travel, ok := travelOf(env)
	if !ok || env.Today.Day() > reportDays {
		return travel, nil, time.Time{}, false
	}
	start, end := lastMonth(env.Today)
	return travel, travel.Between(start, end), start, true
}

func travelCosts(_ any, _ map[string]any, env Env) []Finding {
	_, rides, start, ok := lastMonthRides(env)
	if !ok {
		return nil
	}
	sum := metrics.ByClass(rides)[metrics.ClassBusiness]
	if sum.KM == 0 {
		return nil
	}
	rate := metrics.TravelSettingsOf(env.Settings).KMRate
	return []Finding{{
		Fingerprint: "travel:" + start.Format("2006-01"), Severity: enums.SeverityInfo, Message: "geo.travel_costs",
		Params: map[string]any{
			"trips": sum.Rides, "km": Num(sum.KM, 0), "amount": Money(sum.KM*rate, ""), "month": start.Format("01/2006"),
		},
		Sources: []string{dawarichSvc},
	}}
}

func perDiem(_ any, _ map[string]any, env Env) []Finding {
	travel, ok := travelOf(env)
	if !ok || env.Today.Day() > reportDays {
		return nil
	}
	start, end := lastMonth(env.Today)
	var days, full int
	var amount float64
	for _, d := range metrics.Allowances(travel.Rides, metrics.TravelSettingsOf(env.Settings).Base) {
		if d.Day.Before(start) || d.Day.After(end) {
			continue
		}
		days++
		amount += d.Amount()
		if d.Full {
			full++
		}
	}
	if days == 0 {
		return nil
	}
	return []Finding{{
		Fingerprint: "perdiem:" + start.Format("2006-01"), Severity: enums.SeverityInfo, Message: "geo.per_diem",
		Params:  map[string]any{"days": days, "full": full, "amount": Money(amount, ""), "month": start.Format("01/2006")},
		Sources: []string{dawarichSvc},
	}}
}

// travelUnbilled: a customer's business km of last month, but none of
// their invoices of last or this month has a travel line.
func travelUnbilled(_ any, cfg map[string]any, env Env) []Finding {
	_, rides, start, ok := lastMonthRides(env)
	kimai, ok1 := env.Datasets[kimaiSvc].(*sources.KimaiDataset)
	ninja, ok2 := env.Datasets[ninjaSvc].(*sources.NinjaDataset)
	if !ok || !ok1 || !ok2 {
		return nil
	}
	names := metrics.KimaiCustomerNames(kimai)
	clients := map[string]int64{}
	for _, c := range ninja.Clients {
		clients[strings.ToLower(strings.TrimSpace(c.Name))] = c.ID
	}
	words := stringsSlice(cfg["words"])
	rate := metrics.TravelSettingsOf(env.Settings).KMRate

	var found []Finding
	for customer, sum := range metrics.ByCustomer(rides) {
		client, known := clients[strings.ToLower(strings.TrimSpace(names[customer]))]
		if customer == 0 || !known || billedTravel(ninja, client, start, words) {
			continue
		}
		found = append(found, Finding{
			Fingerprint: fmt.Sprintf("travelbill:%s:%d", start.Format("2006-01"), customer), Severity: enums.SeverityInfo,
			Message: "geo.travel_unbilled",
			Params: map[string]any{"customer": names[customer], "month": start.Format("01/2006"), "km": Num(sum.KM, 0),
				"amount": Money(sum.KM*rate, ninja.Currency)},
			Sources: []string{dawarichSvc, ninjaSvc},
		})
	}
	return found
}

// billedTravel says whether an invoice of the client since from has a
// line with one of the words.
func billedTravel(ninja *sources.NinjaDataset, client int64, from time.Time, words []string) bool {
	for _, inv := range ninja.Invoices {
		day, ok := metrics.ParseDay(inv.Date)
		if inv.ClientID != client || !ok || day.Before(from) {
			continue
		}
		for _, item := range inv.Items {
			if containsAny(item, words) {
				return true
			}
		}
	}
	return false
}

func unplaced(_ any, cfg map[string]any, env Env) []Finding {
	travel, ok := travelOf(env)
	if !ok {
		return nil
	}
	rides := travel.Between(env.Today.AddDate(0, 0, -lookbackDays), env.Today)
	spots := metrics.Unplaced(rides, cfgInt(cfg, "rides"))
	if len(spots) == 0 {
		return nil
	}
	var names []string
	for _, d := range spots {
		if d.Site != nil {
			names = append(names, d.Site.Name)
		}
	}
	return []Finding{{
		Fingerprint: "unplaced", Severity: enums.SeverityInfo, Message: "geo.unplaced",
		Params:    map[string]any{"count": len(spots), "days": lookbackDays, "names": strings.Join(names, ", ")},
		ActionURL: placesURL, ActionLabel: "assign_places",
		Sources: []string{dawarichSvc},
	}}
}

// pluginMissing: the plugin is in use (it has trips last month), but
// business rides of last month are not in it.
func pluginMissing(_ any, _ map[string]any, env Env) []Finding {
	_, rides, start, ok := lastMonthRides(env)
	kimai, ok1 := env.Datasets[kimaiSvc].(*sources.KimaiDataset)
	if !ok || !ok1 || !kimai.Mileage {
		return nil
	}
	used := false
	for _, t := range kimai.MileageTrips {
		if d, ok := metrics.ParseDay(t.Date); ok && !d.Before(start) && d.Before(metrics.MonthStart(env.Today)) {
			used = true
		}
	}
	missing, km := 0, 0.0
	for _, r := range rides {
		if r.Class == metrics.ClassBusiness && r.Reason != metrics.ReasonPlugin && !r.Estimated {
			missing++
			km += r.KM
		}
	}
	if !used || missing == 0 {
		return nil
	}
	return []Finding{{
		Fingerprint: "pluginmissing:" + start.Format("2006-01"), Severity: enums.SeverityInfo, Message: "geo.plugin_missing",
		Params:    map[string]any{"count": missing, "km": Num(km, 0), "month": start.Format("01/2006")},
		ActionURL: strings.TrimRight(kimai.URL, "/") + "/mileage/suggestions", ActionLabel: "open_in_kimai",
		Sources: []string{dawarichSvc, kimaiSvc},
	}}
}

// carPrivateShare: with a business car, more than half of the year's
// car km private rules out the 1 % method.
func carPrivateShare(_ any, cfg map[string]any, env Env) []Finding {
	travel, ok := travelOf(env)
	if !ok || !metrics.TravelSettingsOf(env.Settings).CompanyCar {
		return nil
	}
	year := time.Date(env.Today.Year(), 1, 1, 0, 0, 0, 0, time.UTC)
	share, km := metrics.CarShare(travel.Between(year, env.Today))
	if km < cfgFloat(cfg, "min_km") || share <= cfgFloat(cfg, "private_share") {
		return nil
	}
	return []Finding{{
		Fingerprint: fmt.Sprintf("carshare:%d", env.Today.Year()), Severity: enums.SeverityWarn, Message: "geo.car_private_share",
		Params:  map[string]any{"share": Num(math.Round(share*100), 0), "km": Num(km, 0), "year": env.Today.Year()},
		Sources: []string{dawarichSvc},
	}}
}

func tracksMissing(data *sources.DawarichDataset, _ map[string]any, _ Env) []Finding {
	if data.TracksState != sources.TracksMissing && data.TracksState != sources.TracksFailed {
		return nil
	}
	return []Finding{{
		Fingerprint: "tracks:" + string(data.TracksState), Severity: enums.SeverityInfo, Message: "geo.tracks_" + string(data.TracksState),
		ActionURL: strings.TrimRight(data.URL, "/") + "/map", ActionLabel: "open_in_dawarich",
		Sources: []string{dawarichSvc},
	}}
}
