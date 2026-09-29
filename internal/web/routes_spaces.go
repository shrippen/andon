package web

import (
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/rules"
	"net/http"
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
)

// RegisterSpaceRoutes wires a space's evaluation settings: goals, tax
// values and rule thresholds.
func (d Deps) RegisterSpaceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /spaces/settings", d.authed(d.handleMySpaceSettings))
	mux.HandleFunc("GET /spaces/{id}/settings", d.authed(d.handleSpaceSettings))
	mux.HandleFunc("POST /spaces/{id}/settings", d.authed(d.handleSpaceSettingsSave))
}

func (d Deps) handleMySpaceSettings(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	mine := access.Personal(ctx.Who)
	if mine == nil {
		http.NotFound(w, r)
		return
	}
	http.Redirect(w, r, "/spaces/"+strconv.FormatInt(mine.ID, 10)+"/settings", http.StatusSeeOther)
}

func (d Deps) handleSpaceSettings(w http.ResponseWriter, r *http.Request, ctx Ctx) {
	id, err := pathID(r, "id")
	if err != nil {
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
	_ = d.Page(w, ctx, "space_settings", http.StatusOK, map[string]any{
		"SpaceID": id, "Goals": goals, "Tax": tax, "VAT": asMap(tax["vat"]), "Prepay": asMap(tax["prepayments"]),
		"Costs": asMap(settings["costs"]), "Homelab": asMap(settings["homelab"]), "Billing": asMap(settings["billing"]),
		"Center": metrics.CenterOf(settings), "Centers": centers,
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
	if err != nil {
		http.NotFound(w, r)
		return
	}
	annual := strings.TrimSpace(r.FormValue("annual_due"))
	if annual == "" {
		annual = defaultAnnualDue
	}
	changes := map[string]any{
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
		// Customers whose time is never invoiced (own projects, clubs).
		"billing": map[string]any{"internal": strings.TrimSpace(r.FormValue("billing_internal"))},
		// Mean or median for typical values (payment days, usual traffic).
		"stats": map[string]any{"center": oneOf(r.FormValue("center"), centers)},
		"homelab": map[string]any{
			"power_entity":   strings.TrimSpace(r.FormValue("power_entity")),
			"power_price":    number(r.FormValue("power_price"), 0),
			"hardware_years": number(r.FormValue("hardware_years"), defaultHardwareYears),
			"domain_yearly":  number(r.FormValue("domain_yearly"), 0),
			"cloud_monthly":  number(r.FormValue("cloud_monthly"), 0),
			"hosting_words":  strings.TrimSpace(r.FormValue("hosting_words")),
		},
		"rules": spaces.ParseRules(r.FormValue),
	}
	for k, v := range spaces.ParsePage(r.FormValue) {
		changes[k] = v
	}
	changes[rules.CustomKey] = spaces.ParseCustomRules(r.FormValue)
	changes["maintenance"] = spaces.ParseMaintenance(r.FormValue, r.Form["maint_conn"], time.Local)
	if err := spaces.Update(d.DB, ctx.Who, id, changes, d.clientIP(r)); err != nil {
		d.handleBoardError(w, r, err)
		return
	}
	http.Redirect(w, r, "/spaces/"+strconv.FormatInt(id, 10)+"/settings?saved=1", http.StatusSeeOther)
}
