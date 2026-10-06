package sources

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
)

const (
	dataTTL          = 10 * time.Minute
	testTTL          = time.Second
	demoScheme       = "demo://"
	visitDays        = 120
	secondsPerMinute = 60
)

// TestNotes is the key of a test source's notes: catalog keys of what
// works, but not fully ("the plugin is read-only").
const TestNotes = "notes"

func isDemo(sctx Ctx) bool {
	return len(sctx.URL) >= len(demoScheme) && sctx.URL[:len(demoScheme)] == demoScheme
}

func needSecret(sctx Ctx) (string, error) {
	if sctx.Secret == "" {
		return "", newSourceError("credential.missing")
	}
	return sctx.Secret, nil
}

// windowStart is January 1st of last year: enough for year-over-year
// comparisons.
func windowStart(today time.Time) time.Time {
	return time.Date(today.Year()-1, 1, 1, 0, 0, 0, 0, time.UTC)
}

func kimaiAPI(sctx Ctx) (services.KimaiApi, error) {
	secret, err := needSecret(sctx)
	if err != nil {
		return services.KimaiApi{}, err
	}
	return services.KimaiApi{URL: sctx.URL, Token: secret, Verify: sctx.VerifyTLS}, nil
}

func kimaiSheet(raw any) KimaiSheet {
	m := asMap(raw)
	project := asMap(m["project"])
	customer := asMap(project["customer"])
	activity := asMap(m["activity"])
	customerID := asInt64(customer["id"])
	if customerID == 0 {
		customerID = asInt64(project["customer"])
	}
	return KimaiSheet{
		ID: asInt64(m["id"]), Begin: asStr(m["begin"]), End: asStr(m["end"]),
		Minutes: int(math.Round(asFloat(m["duration"]) / secondsPerMinute)),
		Rate:    asFloat(m["rate"]), HourlyRate: asFloat(m["hourlyRate"]), Billable: boolOr(m["billable"], true), Exported: asBool(m["exported"]),
		ProjectID: refID(m["project"]), CustomerID: customerID, Activity: asStr(activity["name"]),
		UserID: refID(m["user"]),
	}
}

func boolOr(v any, def bool) bool {
	if v == nil {
		return def
	}
	return asBool(v)
}

func kimaiProject(raw any) KimaiProject {
	m := asMap(raw)
	return KimaiProject{
		ID: asInt64(m["id"]), Name: asStr(m["name"]), CustomerID: refID(m["customer"]),
		Budget: asFloat(m["budget"]), TimeBudgetMin: int(math.Round(asFloat(m["timeBudget"]) / secondsPerMinute)),
		BudgetType: asStr(m["budgetType"]), End: asStr(m["end"]), Color: asStr(m["color"]),
	}
}

// KimaiData is the "kimai.data" source: timesheets, projects, customers and
// (if the holiday-bundle plugin is installed) absences/public holidays.
var KimaiData = source{key: "kimai.data", ttl: dataTTL, service: enums.ServiceKimai, fetch: fetchKimai}

func fetchKimai(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoKimai(time.Now()), nil
	}
	api, err := kimaiAPI(sctx)
	if err != nil {
		return nil, err
	}
	data, err := loadKimai(ctx, api, sctx)
	if err != nil {
		return nil, fetchError(err)
	}
	return data, nil
}

func loadKimai(ctx context.Context, api services.KimaiApi, sctx Ctx) (*KimaiDataset, error) {
	today := time.Now().UTC()
	scope := url.Values{}
	if allUsers, _ := sctx.Options["all_users"].(bool); allUsers {
		scope.Set("user", "all")
	}
	scope.Set("begin", windowStart(today).Format("2006-01-02")+"T00:00:00")
	scope.Set("full", "true")

	sheetsRaw, err := api.Pages(ctx, "timesheets", scope)
	if err != nil {
		return nil, err
	}
	projectsRaw, err := api.Get(ctx, "projects", url.Values{"visible": {"3"}})
	if err != nil {
		return nil, err
	}

	projects := make([]KimaiProject, 0, len(asList(projectsRaw)))
	for _, p := range asList(projectsRaw) {
		pm := asMap(p)
		full, err := api.Get(ctx, fmt.Sprintf("projects/%d", asInt64(pm["id"])), nil)
		if err != nil {
			return nil, err
		}
		project := kimaiProject(full)
		if project.Budget != 0 || project.TimeBudgetMin != 0 {
			used := url.Values{}
			for k, v := range scope {
				used[k] = v
			}
			used.Set("projects[]", strconv.FormatInt(project.ID, 10))
			usedSheets, err := api.Pages(ctx, "timesheets", used)
			if err != nil {
				return nil, err
			}
			var money float64
			var minutes float64
			for _, s := range usedSheets {
				sm := asMap(s)
				money += asFloat(sm["rate"])
				minutes += asFloat(sm["duration"])
			}
			project.UsedMoney = money
			project.UsedMinutes = int(math.Round(minutes / secondsPerMinute))
		}
		projects = append(projects, project)
	}

	customersRaw, err := api.Get(ctx, "customers", url.Values{"visible": {"3"}})
	if err != nil {
		return nil, err
	}
	customers := make([]KimaiCustomer, 0, len(asList(customersRaw)))
	for _, c := range asList(customersRaw) {
		cm := asMap(c)
		customers = append(customers, KimaiCustomer{ID: asInt64(cm["id"]), Name: asStr(cm["name"]), Color: asStr(cm["color"])})
	}

	activeRaw, err := api.Get(ctx, "timesheets/active", nil)
	if err != nil {
		return nil, err
	}

	sheets := make([]KimaiSheet, 0, len(sheetsRaw))
	for _, s := range sheetsRaw {
		sheets = append(sheets, kimaiSheet(s))
	}
	active := make([]KimaiSheet, 0, len(asList(activeRaw)))
	for _, s := range asList(activeRaw) {
		active = append(active, kimaiSheet(s))
	}

	data := &KimaiDataset{
		URL: sctx.URL, Timesheets: sheets, Active: active, Projects: projects, Customers: customers,
	}
	loadKimaiHolidays(ctx, api, today, data)
	data.Contract = loadContract(ctx, api)
	// The plugin answers at most 366 days at a time.
	loadMileage(ctx, api, today.AddDate(0, 0, -mileageDays), data)
	return data, nil
}

// mileageDays is how far back the plugin's trips are read.
const mileageDays = 365

// mileageWrite is the ping feature for POST/PATCH /api/mileage/places.
const mileageWrite = "placesWrite"

// mileageRights is what the plugin's ping grants the token.
type mileageRights struct{ view, edit, placesWrite bool }

// mileagePing asks the plugin for the token's rights; ok is false
// without the plugin.
func mileagePing(ctx context.Context, api services.KimaiApi) (mileageRights, bool) {
	ping, err := api.Get(ctx, "mileage/ping", nil)
	if err != nil {
		return mileageRights{}, false
	}
	perms := asMap(asMap(ping)["permissions"])
	out := mileageRights{view: asBool(perms["view"]), edit: asBool(perms["editOwn"])}
	for _, f := range asList(asMap(ping)["features"]) {
		if asStr(f) == mileageWrite {
			out.placesWrite = true
		}
	}
	return out, true
}

// Notes of the connection test on the plugin (catalog keys).
const (
	noteMileageRead = "test.mileage_read_only"
	noteMileageOld  = "test.mileage_old"
)

// mileageNotes says what the plugin lacks for Andon to write to it.
func mileageNotes(r mileageRights) []string {
	switch {
	case !r.view:
		return nil
	case !r.edit:
		return []string{noteMileageRead}
	case !r.placesWrite:
		return []string{noteMileageOld}
	}
	return nil
}

// loadMileage reads the mileage plugin: its places and trips since from.
// Without the plugin (404) or its permission it stays empty.
func loadMileage(ctx context.Context, api services.KimaiApi, from time.Time, data *KimaiDataset) {
	ping, ok := mileagePing(ctx, api)
	if !ok || !ping.view {
		return
	}
	data.PlacesWrite, data.MileageEdit = ping.placesWrite, ping.edit

	placesRaw, err := api.Get(ctx, "mileage/places", nil)
	if err != nil {
		return
	}
	for _, p := range asList(placesRaw) {
		pm := asMap(p)
		data.Places = append(data.Places, KimaiPlace{
			ID: asInt64(pm["id"]), Name: asStr(pm["name"]), Type: asStr(pm["type"]), CustomerID: asInt64(pm["customerId"]),
			Lat: asFloat(pm["latitude"]), Lon: asFloat(pm["longitude"]), Radius: asFloat(pm["radius"]),
			AreaID: asInt64(pm["dawarichAreaId"]), PlaceID: asInt64(pm["dawarichPlaceId"]),
		})
	}

	tripsRaw, err := api.Get(ctx, "mileage/trips", url.Values{"from": {from.Format("2006-01-02")}, "to": {time.Now().UTC().Format("2006-01-02")}})
	if err != nil {
		return
	}
	for _, t := range asList(tripsRaw) {
		tm := asMap(t)
		data.MileageTrips = append(data.MileageTrips, KimaiMileageTrip{
			ID: asInt64(tm["id"]), Date: asStr(tm["date"]), Departure: asStr(tm["departure"]), Arrival: asStr(tm["arrival"]),
			Purpose: asStr(tm["purpose"]), KM: asFloat(tm["totalKm"]), Project: asInt64(tm["project"]), Timesheet: asInt64(tm["timesheet"]),
		})
	}
	data.Mileage = true
}

// loadKimaiHolidays reads the kimai-holiday-bundle: absences and public
// holidays. A missing plugin is fine (data.HolidayBundle stays false).
func loadKimaiHolidays(ctx context.Context, api services.KimaiApi, today time.Time, data *KimaiDataset) {
	for _, year := range []int{today.Year() - 1, today.Year()} {
		absencesRaw, err := api.Get(ctx, "holiday/absences", url.Values{"year": {strconv.Itoa(year)}})
		if err != nil {
			if _, ok := err.(services.ApiMissing); ok {
				return
			}
			return
		}
		for _, a := range asList(absencesRaw) {
			am := asMap(a)
			data.Absences = append(data.Absences, KimaiAbsence{
				Start: asStr(am["startDate"]), End: asStr(am["endDate"]), Type: asStr(am["type"]),
				Status: asStr(am["status"]), HalfDay: asBool(am["halfDay"]),
			})
		}

		holidaysRaw, err := api.Get(ctx, "holiday/public-holidays", url.Values{"year": {strconv.Itoa(year)}})
		if err != nil {
			return
		}
		for _, h := range asList(holidaysRaw) {
			hm := asMap(h)
			data.Holidays = append(data.Holidays, KimaiHoliday{
				Date: asStr(hm["date"]), Name: asStr(hm["name"]), HalfDay: asBool(hm["halfDay"]),
			})
		}
	}
	data.HolidayBundle = true
}

// KimaiTest is the "kimai.test" source: a lightweight connection check.
var KimaiTest = source{key: "kimai.test", ttl: testTTL, service: enums.ServiceKimai, fetch: fetchKimaiTest}

func fetchKimaiTest(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return map[string]any{"version": "demo"}, nil
	}
	api, err := kimaiAPI(sctx)
	if err != nil {
		return nil, err
	}
	body, err := api.Get(ctx, "version", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	out := map[string]any{"version": asStr(asMap(body)["version"])}
	if ping, ok := mileagePing(ctx, api); ok {
		out[TestNotes] = mileageNotes(ping)
	}
	return out, nil
}
