package rules

// Rules of the inventory category (caps.Inventory): they read the shape
// every inventory fills (sources.InventoryData), so they run for each
// inventory connection and need no copy per service.
//
//	inventory ─ warranty, GWG, receipt ────────────────┐ per connection
//	inventory × Invoice Ninja ─ purchase without expense ┤ cross
//	                          ─ expense with another receipt (Kept)
//	inventory × Home Assistant ─ device worth replacing ─┘ cross

import (
	"slices"
	"strings"
	"time"

	"andon/internal/caps"
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const (
	gwgLimitNet = 800.0
	vatFactor   = 1.19
	openIn      = "open_in_"
)

func init() {
	inv := string(caps.Inventory)

	Register("inventory.warranty_expiring", inv, map[string]any{"info_days": 60.0, "warn_days": 14.0}, onInventory(warrantyExpiring))

	Register("inventory.gwg_hint", inv, map[string]any{"cost_is_gross": true}, onInventory(gwgHint))

	// A purchase without receipt or invoice: the tax office and a warranty
	// claim want one. Only inventories that keep receipts (Kept) say so.
	Register("inventory.receipt_missing", inv, map[string]any{"days": 365.0, "min_amount": 50.0}, onInventory(receiptMissing))

	Register("inventory.expense_missing", Cross, map[string]any{"days": 365.0, "tolerance": 0.05, "date_window": 14.0}, expenseMissing)

	Register("inventory.replace_worth", Cross, map[string]any{"new_watts": 15.0, "new_cost": 400.0, "max_months": 36.0}, replaceWorth)
}

// onInventory adapts a rule over the inventory shape: any dataset of the
// category runs it, others find nothing.
func onInventory(run func(data *sources.InventoryData, cfg map[string]any, env Env) []Finding) RuleFunc {
	return func(raw any, cfg map[string]any, env Env) []Finding {
		src, ok := raw.(sources.InventorySource)
		if !ok || isNil(src) {
			return nil
		}
		return run(src.Inventory(), cfg, env)
	}
}

// RecentPurchases returns assets purchased within `days` with a known
// cost — used by inventory.expense_missing.
func RecentPurchases(data *sources.InventoryData, today time.Time, days int) []sources.Asset {
	since := today.AddDate(0, 0, -days)
	var out []sources.Asset
	for _, a := range data.Assets {
		d, ok := metrics.ParseDay(a.PurchaseDate)
		if ok && !d.Before(since) && a.PurchaseCost != 0 {
			out = append(out, a)
		}
	}
	return out
}

func warrantyExpiring(data *sources.InventoryData, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, a := range data.Assets {
		ends, ok := metrics.ParseDay(a.WarrantyExpires)
		if !ok {
			continue
		}
		daysLeft := int(ends.Sub(env.Today).Hours() / 24)
		if daysLeft < 0 || daysLeft > cfgInt(cfg, "info_days") {
			continue
		}
		level := enums.SeverityInfo
		if daysLeft <= cfgInt(cfg, "warn_days") {
			level = enums.SeverityWarn
		}
		found = append(found, Finding{
			Fingerprint: "warranty:" + a.ID, Severity: level, Message: "inventory.warranty",
			Params:    map[string]any{"asset": a.Name, "tag": a.Tag, "day": Day(ends)},
			ActionURL: a.Link, ActionLabel: openIn + string(data.Service),
			Due: ends.Format("2006-01-02"), Sources: []string{string(data.Service)},
		})
	}
	return found
}

func gwgHint(data *sources.InventoryData, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, a := range data.Assets {
		bought, ok := metrics.ParseDay(a.PurchaseDate)
		if !ok || bought.Year() != env.Today.Year() {
			continue
		}
		net := a.PurchaseCost
		if cfgBool(cfg, "cost_is_gross") {
			net /= vatFactor
		}
		if net <= gwgLimitNet {
			continue
		}
		found = append(found, Finding{
			Fingerprint: "gwg:" + a.ID, Severity: enums.SeverityInfo, Message: "inventory.gwg",
			Params:    map[string]any{"asset": a.Name, "net": Money(net, "")},
			ActionURL: a.Link, ActionLabel: openIn + string(data.Service),
			Sources: []string{string(data.Service)},
		})
	}
	return found
}

func receiptMissing(data *sources.InventoryData, cfg map[string]any, env Env) []Finding {
	since := env.Today.AddDate(0, 0, -cfgInt(cfg, "days"))
	var found []Finding
	for _, a := range data.Assets {
		bought, ok := metrics.ParseDay(a.PurchaseDate)
		if a.Receipt != sources.ReceiptNone || !ok || bought.Before(since) || a.PurchaseCost < cfgFloat(cfg, "min_amount") {
			continue
		}
		found = append(found, Finding{
			Fingerprint: "receipt:" + a.ID, Severity: enums.SeverityInfo, Message: "inventory.receipt_missing",
			Params:    map[string]any{"asset": a.Name, "amount": Money(a.PurchaseCost, ""), "day": Day(bought)},
			ActionURL: a.Link, ActionLabel: openIn + string(data.Service),
			Sources: []string{string(data.Service)},
		})
	}
	return found
}

func expenseMissing(_ any, cfg map[string]any, env Env) []Finding {
	assets, ok1 := metrics.InventoryOf(env.Datasets)
	ninja, ok2 := env.Datasets[string(enums.ServiceInvoiceNinja)].(*sources.NinjaDataset)
	if !ok1 || !ok2 {
		return nil
	}
	slot := linkSlot(env.Options[string(enums.ServiceInvoiceNinja)])
	var found []Finding
	for _, asset := range RecentPurchases(assets, env.Today, cfgInt(cfg, "days")) {
		bought, ok := metrics.ParseDay(asset.PurchaseDate)
		if !ok || bought.Year() != env.Today.Year() {
			continue
		}
		cost := asset.PurchaseCost
		tolerance := cost * cfgFloat(cfg, "tolerance")
		window := cfgInt(cfg, "date_window")
		var matched *sources.NinjaExpense
		for i, e := range ninja.Expenses {
			near := absF(e.Amount-cost) <= tolerance || absF(e.Amount-e.Tax-cost) <= tolerance
			if !near {
				continue
			}
			d, ok := metrics.ParseDay(e.Date)
			if ok && absDays(d, bought) <= window {
				matched = &ninja.Expenses[i]
				break
			}
		}
		if matched != nil {
			if f, ok := receiptDiffers(asset, *matched, slot, assets.Service); ok {
				found = append(found, f)
			}
			continue
		}
		found = append(found, Finding{
			Fingerprint: "expense:" + asset.ID, Severity: enums.SeverityInfo, Message: "inventory.expense_missing",
			Params:  map[string]any{"asset": asset.Name, "amount": Money(cost, ""), "day": Day(bought)},
			Sources: []string{string(assets.Service), string(enums.ServiceInvoiceNinja)},
		})
	}
	return found
}

// optLinkSlot is Invoice Ninja's option naming the expense field with
// the Paperless links (services/receipts).
const optLinkSlot = "receipt_link_slot"

// linkSlot reads it: 1–4, 0 none.
func linkSlot(opts map[string]any) int {
	n := cfgInt(opts, optLinkSlot)
	if n < 1 || n > sources.NinjaSlots {
		return 0
	}
	return n
}

// receiptDiffers: the inventory proves the purchase with Paperless
// documents (Kept), the expense found links others. Both sides need a
// link; one without says nothing here (inventory.receipt_missing,
// receipts).
//
//	Kept Monitor ─ invoice doc 4 ┐
//	EX-0040 ─ link doc 5 ────────┴─► differs
func receiptDiffers(asset sources.Asset, e sources.NinjaExpense, slot int, from enums.ServiceType) (Finding, bool) {
	if slot == 0 || len(asset.ReceiptDocs) == 0 {
		return Finding{}, false
	}
	linked := metrics.DocIDs(e.Custom[slot-1])
	if len(linked) == 0 || slices.ContainsFunc(linked, func(id int64) bool { return slices.Contains(asset.ReceiptDocs, id) }) {
		return Finding{}, false
	}
	return Finding{
		Fingerprint: "receiptdoc:" + asset.ID, Severity: enums.SeverityInfo, Message: "inventory.receipt_differs",
		Params:    map[string]any{"asset": asset.Name, "number": e.Number},
		ActionURL: asset.Link, ActionLabel: openIn + string(from),
		Sources: []string{string(from), string(enums.ServiceInvoiceNinja)},
	}, true
}

func replaceWorth(_ any, cfg map[string]any, env Env) []Finding {
	inv, ok1 := metrics.InventoryOf(env.Datasets)
	hass, ok2 := env.Datasets[string(enums.ServiceHomeAssistant)].(*sources.HassDataset)
	if !ok1 || !ok2 {
		return nil
	}
	tibber, _ := env.Datasets[string(enums.ServiceTibber)].(*sources.TibberDataset)
	price := metrics.PowerPrice(tibber, metrics.HomelabSettingsOf(env.Settings).PowerPrice)
	var found []Finding
	for _, r := range metrics.Replacements(inv, hass, price, cfgFloat(cfg, "new_watts"), cfgFloat(cfg, "new_cost"), env.Today) {
		if r.PaybackMonths > cfgInt(cfg, "max_months") {
			continue
		}
		found = append(found, Finding{Fingerprint: "replace:" + strings.ToLower(r.Asset), Severity: enums.SeverityInfo,
			Message: "inventory.replace_worth", Params: map[string]any{"name": r.Asset, "years": Num(r.AgeYears, 0), "watts": Num(r.Watts, 0),
				"yearly": Money(r.YearlyCost, ""), "months": r.PaybackMonths, "new_watts": Num(cfgFloat(cfg, "new_watts"), 0), "new_cost": Money(cfgFloat(cfg, "new_cost"), "")},
			Sources: []string{string(inv.Service), string(enums.ServiceHomeAssistant)}})
	}
	return found
}
