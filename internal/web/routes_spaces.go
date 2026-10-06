package web

import (
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/rules"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/spaces"
)

const (
	defaultHoursPerDay   = 8.0
	defaultIncomeTaxRate = 0.3
	defaultAnnualDue     = "07-31"
)

var (
	vatMethods   = []string{"ist", "soll"}
	vatIntervals = []string{"monthly", "quarterly"}
	centers      = []string{string(metrics.CenterMean), string(metrics.CenterMedian)}
	travelBases  = []metrics.TravelBase{metrics.BaseHome, metrics.BaseWork}
)

// RegisterSpaceRoutes wires a space's evaluation settings: goals, tax
// values and rule thresholds.
func (d Deps) RegisterSpaceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /spaces/settings", d.authed(d.handleMySpaceSettings))
	mux.HandleFunc("GET /spaces/{id}/settings", d.authed(d.handleSpaceSettingsFirst))
	mux.HandleFunc("GET /spaces/{id}/settings/{section}", d.authed(d.handleSpaceSettings))
	mux.HandleFunc("POST /spaces/{id}/settings/{section}", d.authed(d.handleSpaceSettingsSave))
}

func (d Deps) handleMySpaceSettings(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	mine := access.Personal(ctx.Who)
	if mine == nil {
		http.NotFound(w, r)
		return
	}
	section := r.URL.Query().Get("section")
	if !slices.Contains(spaceSections, section) {
		section = sectionPage
	}
	http.Redirect(w, r, sectionPath(mine.ID, section), http.StatusSeeOther)
}

// handleSpaceSettingsFirst opens a space's first settings section.
func (d Deps) handleSpaceSettingsFirst(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, sectionPath(id, sectionPage), http.StatusSeeOther)
}

// sectionOf is the settings section a request names, "" for none known.
func sectionOf(r *http.Request) string {
	section := r.PathValue("section")
	if !slices.Contains(spaceSections, section) {
		return ""
	}
	return section
}

func (d Deps) handleSpaceSettings(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	section := sectionOf(r)
	if err != nil || section == "" {
		http.NotFound(w, r)
		return
	}
	settings, err := spaces.Settings(d.DB, ctx.Who, id)
	if err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	goals := asMap(settings["goals"])
	tax := asMap(settings["tax"])
	maint := spaces.MaintenanceOf(settings)
	chosen := map[int64]bool{}
	for _, id := range maint.Connections {
		chosen[id] = true
	}
	var conns []connections.View
	if all, err := connections.Listing(d.DB, ctx.Who, enums.RightView); err == nil {
		for _, c := range all {
			if c.SpaceID == id {
				conns = append(conns, c)
			}
		}
	}
	var name string
	if ref, err := access.SpaceOf(d.DB, ctx.Who, id); err == nil && ref != nil {
		name = ref.Name
	}
	_ = d.Page(w, ctx, "space_settings", http.StatusOK, map[string]any{
		"SpaceID": id, "SpaceName": name, "Section": section, "Goals": goals, "Tax": tax, "VAT": asMap(tax["vat"]), "Prepay": asMap(tax["prepayments"]),
		"Costs": asMap(settings["costs"]), "Homelab": asMap(settings["homelab"]), "Billing": asMap(settings["billing"]),
		"Center": metrics.CenterOf(settings), "Centers": centers,
		"Travel": metrics.TravelSettingsOf(settings), "TravelBases": travelBases, "FuelWords": strings.Join(metrics.TravelSettingsOf(settings).FuelWords, ", "),
		"RuleGroups": spaces.RuleGroups(settings), "Methods": vatMethods, "Intervals": vatIntervals,
		"Saved": r.URL.Query().Has("saved"), "Page": spaces.PageOf(settings), "NavText": spaces.NavText(spaces.PageOf(settings)),
		"Custom": spaces.CustomRows(settings), "Ops": rules.CustomOps, "Services": enums.Services, "Levels": severityLevels,
		"Maint": maint, "MaintUntil": maint.UntilInput(time.Local), "MaintConns": chosen, "Conns": conns, "Now": time.Now(),
	})
}

func asMap(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

// defaultHardwareYears is the useful life hardware is written off over.
const defaultHardwareYears = 5

func number(raw string, fallback float64) float64 {
	n, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(raw), ",", "."), 64)
	if err != nil {
		return fallback
	}
	return n
}

func oneOf(value string, allowed []string) string {
	for _, a := range allowed {
		if a == value {
			return value
		}
	}
	return allowed[0]
}

func (d Deps) handleSpaceSettingsSave(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	section := sectionOf(r)
	if err != nil || section == "" {
		http.NotFound(w, r)
		return
	}
	if err := spaces.Update(d.DB, ctx.Who, id, sectionChanges(r, section), d.clientIP(r)); err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	http.Redirect(w, r, sectionPath(id, section)+"?saved=1", http.StatusSeeOther)
}

// sectionChanges reads one section's form into the settings keys it owns;
// the other sections' keys stay as they are.
func sectionChanges(r *http.Request, section string) map[string]any {
	_ = r.ParseForm() // r.Form below; a bad body leaves the fields empty
	switch section {
	case sectionPage:
		return spaces.ParsePage(r.FormValue)
	case sectionMaintenance:
		return map[string]any{"maintenance": spaces.ParseMaintenance(r.FormValue, r.Form["maint_conn"], time.Local)}
	case sectionRules:
		return map[string]any{
			// Mean or median for typical values (payment days, usual traffic).
			"stats":         map[string]any{"center": oneOf(r.FormValue("center"), centers)},
			"rules":         spaces.ParseRules(r.FormValue),
			rules.CustomKey: spaces.ParseCustomRules(r.FormValue),
		}
	}

	annual := strings.TrimSpace(r.FormValue("annual_due"))
	if annual == "" {
		annual = defaultAnnualDue
	}
	return map[string]any{
		"goals": map[string]any{
			"revenue_year": number(r.FormValue("revenue_year"), 0),
		},
		"tax": map[string]any{
			"vat": map[string]any{
				"method":          oneOf(r.FormValue("vat_method"), vatMethods),
				"return_interval": oneOf(r.FormValue("vat_interval"), vatIntervals),
				"extension":       checked(r, "vat_extension"),
			},
			"prepayments":     map[string]any{"amount": number(r.FormValue("prepayment"), 0)},
			"annual_due":      annual,
			"income_tax_rate": number(r.FormValue("income_tax_rate"), defaultIncomeTaxRate),
		},
		"costs": map[string]any{"fixed_monthly": number(r.FormValue("fixed_monthly"), 0), "hourly_cost": number(r.FormValue("hourly_cost"), 0)},
		// Where business travel starts, the km rate, a business car.
		"travel": map[string]any{
			"base":        oneOf(r.FormValue("travel_base"), []string{string(metrics.BaseHome), string(metrics.BaseWork)}),
			"km_rate":     number(r.FormValue("km_rate"), metrics.DefaultKMRate),
			"company_car": checked(r, "company_car"),
			"fuel_words":  strings.TrimSpace(r.FormValue("fuel_words")),
		},
		// Customers whose time is never invoiced (own projects, clubs).
		"billing": map[string]any{"internal": strings.TrimSpace(r.FormValue("billing_internal"))},
		"homelab": map[string]any{
			"power_entity":   strings.TrimSpace(r.FormValue("power_entity")),
			"power_price":    number(r.FormValue("power_price"), 0),
			"hardware_years": number(r.FormValue("hardware_years"), defaultHardwareYears),
			"domain_yearly":  number(r.FormValue("domain_yearly"), 0),
			"cloud_monthly":  number(r.FormValue("cloud_monthly"), 0),
			"hosting_words":  strings.TrimSpace(r.FormValue("hosting_words")),
		},
	}
}
