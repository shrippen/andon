package sources

import (
	"cmp"
	"context"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
)

func snipeAPI(sctx Ctx) (services.SnipeApi, error) {
	secret, err := needSecret(sctx)
	if err != nil {
		return services.SnipeApi{}, err
	}
	return services.SnipeApi{URL: sctx.URL, Token: secret, Verify: sctx.VerifyTLS}, nil
}

func nameOf(v any) string {
	if m, ok := v.(map[string]any); ok {
		return asStr(m["name"])
	}
	return asStr(v)
}

func snipeAsset(raw any) SnipeAsset {
	m := asMap(raw)
	status := asMap(m["status_label"])
	name := asStr(m["name"])
	if name == "" {
		name = nameOf(m["model"])
	}
	return SnipeAsset{
		ID: asInt64(m["id"]), Name: name, Tag: asStr(m["asset_tag"]), Model: nameOf(m["model"]),
		Category: nameOf(m["category"]), Status: asStr(status["name"]),
		Deployable: asStr(status["status_meta"]) == "deployable", Assigned: m["assigned_to"] != nil,
		PurchaseDate: day(m["purchase_date"]), PurchaseCost: snipeCost(m["purchase_cost"]),
		WarrantyExpires: day(m["warranty_expires"]), EOLDate: day(m["asset_eol_date"]),
		NextAudit:  day(m["next_audit_date"]),
		LastChange: cmp.Or(day(m["last_checkin"]), day(m["last_checkout"]), day(m["updated_at"])),
		AssignedTo: nameOf(m["assigned_to"]), ExpectedCheckin: day(m["expected_checkin"]), Serial: asStr(m["serial"]),
	}
}

// SnipeData is the "snipeit.data" source: assets, licenses, consumables
// and overdue audits.
var SnipeData = source{key: "snipeit.data", ttl: dataTTL, service: enums.ServiceSnipeIT, fetch: fetchSnipe}

func fetchSnipe(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoSnipe(time.Now()), nil
	}
	api, err := snipeAPI(sctx)
	if err != nil {
		return nil, err
	}
	data, err := loadSnipe(ctx, api, sctx)
	if err != nil {
		return nil, fetchError(err)
	}
	return data, nil
}

func loadSnipe(ctx context.Context, api services.SnipeApi, sctx Ctx) (*SnipeDataset, error) {
	hardware, err := api.Rows(ctx, "hardware", nil)
	if err != nil {
		return nil, err
	}
	assets := make([]SnipeAsset, 0, len(hardware))
	for _, a := range hardware {
		assets = append(assets, snipeAsset(a))
	}

	licensesRaw, err := api.Rows(ctx, "licenses", nil)
	if err != nil {
		return nil, err
	}
	var licenses []SnipeLicense
	for _, l := range licensesRaw {
		lm := asMap(l)
		expires := day(lm["expiration_date"])
		if expires == "" {
			expires = day(lm["termination_date"])
		}
		licenses = append(licenses, SnipeLicense{
			ID: asInt64(lm["id"]), Name: asStr(lm["name"]), Expires: expires,
			Seats: int(asFloat(lm["seats"])), Free: int(asFloat(lm["free_seats_count"])),
		})
	}

	consumablesRaw, err := api.Rows(ctx, "consumables", nil)
	if err != nil {
		return nil, err
	}
	var consumables []SnipeConsumable
	for _, c := range consumablesRaw {
		cm := asMap(c)
		consumables = append(consumables, SnipeConsumable{
			ID: asInt64(cm["id"]), Name: asStr(cm["name"]),
			Remaining: int(asFloat(cm["remaining"])), Min: int(asFloat(cm["min_amt"])),
		})
	}

	overdueRaw, err := api.Rows(ctx, "hardware/audit/overdue", nil)
	if err != nil {
		return nil, err
	}
	overdue := make([]int64, 0, len(overdueRaw))
	for _, a := range overdueRaw {
		overdue = append(overdue, asInt64(asMap(a)["id"]))
	}

	return &SnipeDataset{
		URL: sctx.URL, Assets: assets, Licenses: licenses, Consumables: consumables, AuditOverdue: overdue,
	}, nil
}

// SnipeTest is the "snipeit.test" source: a lightweight connection check.
var SnipeTest = source{key: "snipeit.test", ttl: testTTL, service: enums.ServiceSnipeIT, fetch: fetchSnipeTest}

func fetchSnipeTest(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return map[string]any{"version": "demo"}, nil
	}
	api, err := snipeAPI(sctx)
	if err != nil {
		return nil, err
	}
	me, err := api.Get(ctx, "users/me", nil)
	if err != nil {
		return nil, fetchError(err)
	}
	return map[string]any{"version": nil, "user": asStr(asMap(me)["username"])}, nil
}

// snipeMoney reads a cost as Snipe-IT formats it, by its separator
// setting: "1,999.00" or "1.234,56". The last separator is the decimal
// one unless three digits follow it ("1,999" is thousands); a missing
// number is 0.
func snipeMoney(s string) float64 {
	s = strings.TrimSpace(s)
	i := strings.LastIndexAny(s, ".,")
	if i >= 0 && len(s)-i-1 == thousandsDigits {
		i = -1
	}
	whole, frac := s, ""
	if i >= 0 {
		whole, frac = s[:i], s[i+1:]
	}
	whole = strings.NewReplacer(".", "", ",", "").Replace(whole)
	f, _ := strconv.ParseFloat(whole+"."+frac, 64)
	return f
}

// snipeCost is a cost sent as number or as Snipe-IT's formatted text.
func snipeCost(v any) float64 {
	if f, ok := v.(float64); ok {
		return f
	}
	return snipeMoney(asStr(v))
}

// thousandsDigits follow a thousands separator.
const thousandsDigits = 3
