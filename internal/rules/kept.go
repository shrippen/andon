package rules

// Kept's own rules; warranty, GWG and depreciation come from the
// inventory category (inventory.go).
//
//	kept.due         a date of Kept's agenda is close, due or overdue, with
//	                 Kept's own lead time (expiry, warranty, document,
//	                 maintenance, return): a deadline in Andon's calendar
//	inventory.split  two inventories in one Verbund (moving from Snipe-IT to
//	                 Kept): bought assets only the second one knows are
//	                 left out of depreciation
//	cross.kept_paperless  Kept's receipt links against the Verbund's
//	                 Paperless: another Paperless, a document gone, the
//	                 location without its Paperless connection

import (
	"slices"
	"strings"

	"andon/internal/caps"
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// Kept's agenda states, as Kept names them.
const (
	keptExpiring = "expiring"
	keptDue      = "due"
	keptOverdue  = "overdue"
)

// keptLevels is the hint's severity per agenda state; "upcoming" is no
// hint yet.
var keptLevels = map[string]enums.Severity{
	keptExpiring: enums.SeverityInfo,
	keptDue:      enums.SeverityWarn,
	keptOverdue:  enums.SeverityCritical,
}

func init() {
	// Warranty ends are inventory.warranty_expiring's (every inventory);
	// returns are lending, not deadlines: both chosen in the rule.
	Register("kept.due", string(enums.ServiceKept), map[string]any{
		"kinds": []any{"thing_expiry", "document", "schedule"}}, on(keptDueDates))

	Register("inventory.split", Cross, nil, inventorySplit)

	Register("cross.kept_paperless", Cross, nil, keptPaperless)
}

func keptDueDates(data *sources.KeptDataset, cfg map[string]any, env Env) []Finding {
	kinds := stringsSlice(cfg["kinds"])
	var found []Finding
	for _, d := range data.Agenda {
		level, ok := keptLevels[d.State]
		if !ok || !slices.Contains(kinds, d.Kind) {
			continue
		}
		link := data.DueLink(d)
		found = append(found, Finding{
			Fingerprint: "due:" + d.Kind + ":" + d.Subject + ":" + d.Title + ":" + d.Due, Severity: level,
			Message: "kept.due_" + d.State,
			Params: map[string]any{"subject": d.Subject, "title": d.Title, "kind": map[string]any{"$t": "kept.kind." + d.Kind},
				"day": DayStr(d.Due)},
			ActionURL: link, ActionLabel: openIn + string(enums.ServiceKept),
			Due: d.Due, Sources: []string{string(enums.ServiceKept)},
		})
	}
	return found
}

// inventorySplit: the Verbund counts its first inventory (Kept before
// Snipe-IT, caps.Members); bought assets only a later one has are named,
// as depreciation and the export leave them out.
func inventorySplit(_ any, _ map[string]any, env Env) []Finding {
	first, ok := metrics.InventoryOf(env.Datasets)
	if !ok {
		return nil
	}
	known := map[string]bool{}
	for _, a := range first.Assets {
		for _, k := range assetKeys(a) {
			known[k] = true
		}
	}
	var found []Finding
	for _, s := range caps.Members(caps.Inventory) {
		src, ok := env.Datasets[string(s)].(sources.InventorySource)
		if !ok || isNil(src) || s == first.Service {
			continue
		}
		var names []string
		for _, a := range src.Inventory().Assets {
			if a.PurchaseDate != "" && a.PurchaseCost > 0 && !slices.ContainsFunc(assetKeys(a), func(k string) bool { return known[k] }) {
				names = append(names, a.Name)
			}
		}
		if len(names) == 0 {
			continue
		}
		found = append(found, Finding{Fingerprint: "split:" + string(s), Severity: enums.SeverityWarn, Message: "inventory.split",
			Params: map[string]any{"count": len(names), "names": shortList(names),
				"first": map[string]any{"$t": "service." + string(first.Service)}, "other": map[string]any{"$t": "service." + string(s)}},
			Sources: []string{string(first.Service), string(s)}})
	}
	return found
}

// assetKeys match an asset across inventories: its serial and its name
// (lower case); one inventory may lack the serial.
func assetKeys(a sources.Asset) []string {
	keys := []string{"n:" + strings.ToLower(a.Name)}
	if a.Serial != "" {
		keys = append(keys, "s:"+strings.ToLower(a.Serial))
	}
	return keys
}

// keptPaperless checks Kept's Paperless links against the Verbund's
// Paperless: one hint per problem, naming the things.
//
//	link into another Paperless ─► elsewhere
//	document id unknown there   ─► gone
//	location not connected      ─► off (links unchecked, new ones impossible)
func keptPaperless(_ any, _ map[string]any, env Env) []Finding {
	kept, ok1 := env.Datasets[string(enums.ServiceKept)].(*sources.KeptDataset)
	paperless, ok2 := env.Datasets[string(enums.ServicePaperless)].(*sources.PaperlessDataset)
	if !ok1 || !ok2 || kept == nil || paperless == nil {
		return nil
	}
	ours := hostOf(paperless.URL)
	var elsewhere, gone, off []string
	for _, t := range kept.Things {
		if t.PaperlessMissing && t.Bought != "" {
			off = appendOnce(off, t.Location)
		}
		for _, r := range t.Receipts {
			switch {
			case r.DocID == 0:
			case hostOf(r.Paperless) != ours:
				elsewhere = appendOnce(elsewhere, t.Name)
			case !paperless.Has(r.DocID):
				gone = appendOnce(gone, t.Name)
			}
		}
	}

	var found []Finding
	add := func(kind string, names []string, level enums.Severity) {
		if len(names) == 0 {
			return
		}
		found = append(found, Finding{Fingerprint: "paperless:" + kind, Severity: level, Message: "cross.kept_paperless_" + kind,
			Params:  map[string]any{"count": len(names), "names": shortList(names)},
			Sources: []string{string(enums.ServiceKept), string(enums.ServicePaperless)}})
	}
	add("gone", gone, enums.SeverityWarn)
	add("elsewhere", elsewhere, enums.SeverityInfo)
	add("off", off, enums.SeverityInfo)
	return found
}

func appendOnce(list []string, s string) []string {
	if s == "" || slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}
