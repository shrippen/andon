package widgets

// "subscriptions": what recurring costs a month and what is debited next.
// Wallos is the list of record when connected; Sure's recurring payments
// fill in without it, and name what Wallos lacks when both are there.

import (
	"slices"
	"sort"
	"strings"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/rules"
	"andon/internal/sources"
)

// SubsConfig is the "subscriptions" widget's config.
type SubsConfig struct {
	Limit      int
	ByPrice    bool     // dearest first instead of next debit
	Yearly     bool     // costs per year
	Categories []string // only these (lower case), empty = all
}

const (
	defaultSubRows = 5
	peerWallos     = "wallos"
)

func decodeSubs(raw map[string]any) any {
	return SubsConfig{Limit: clampInt(asInt(raw["limit"], defaultSubRows), 1, 30), ByPrice: raw["sort"] == "price",
		Yearly: asBool(raw["yearly"]), Categories: lowerList(raw["categories"])}
}

// SubRow is one subscription as listed.
type SubRow struct {
	Name, Next       string
	Price, Monthly   float64
	Category, Detail string
}

func subsView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg := cfgAny.(SubsConfig)
	today := todayOf(ctx).Format("2006-01-02")
	wallos, hasWallos := results[peerWallos].(*sources.WallosDataset)
	sure, hasSure := results[peerSure].(*sources.SureDataset)

	var rows []SubRow
	out := map[string]any{}
	switch {
	case hasWallos:
		out["Source"], out["Currency"], out["URL"] = "wallos", wallos.Currency, wallos.URL
		for _, s := range wallos.Subs {
			if !s.Inactive {
				rows = append(rows, SubRow{Name: s.Name, Next: s.Next, Price: s.Price, Monthly: s.Monthly, Category: s.Category})
			}
		}
		if hasSure {
			rule := ruleConfig("cross.wallos_missing", ctx.Settings)
			var names []string
			for _, r := range metrics.NotInWallos(sure, wallos, floatOf(rule["max_monthly"]), asStringList(rule["ignore_names"])) {
				names = append(names, r.Name)
			}
			out["Missing"] = strings.Join(names, ", ")
		}
	case hasSure:
		out["Source"], out["Currency"] = "sure", sure.Currency
		categories := sureCategories(sure)
		for _, s := range metrics.Subscriptions(sure, nil, rules.Usages(rules.Env{})) {
			rows = append(rows, SubRow{Name: s.Name, Next: s.Next, Price: s.Monthly, Monthly: s.Monthly,
				Category: categories[strings.ToLower(s.Name)]})
		}
	default:
		return out
	}

	if len(cfg.Categories) > 0 {
		kept := rows[:0]
		for _, r := range rows {
			if slices.Contains(cfg.Categories, strings.ToLower(r.Category)) {
				kept = append(kept, r)
			}
		}
		rows = kept
	}
	total := 0.0
	for i, r := range rows {
		total += r.Monthly
		if cfg.Yearly {
			rows[i].Price = r.Monthly * monthsPerYearF
		}
	}
	if cfg.Yearly {
		total *= monthsPerYearF
	}
	out["Monthly"] = float64(int(total*cents+0.5)) / cents
	out["Count"], out["Yearly"] = len(rows), cfg.Yearly
	if cfg.ByPrice {
		sort.SliceStable(rows, func(i, j int) bool { return rows[i].Monthly > rows[j].Monthly })
		out["Rows"] = rows[:min(len(rows), cfg.Limit)]
		return out
	}
	// Next debits first; past or unknown dates last.
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].Next, rows[j].Next
		if (a >= today) != (b >= today) {
			return a >= today
		}
		return a < b
	})
	out["Rows"] = rows[:min(len(rows), cfg.Limit)]
	return out
}

// sureCategories maps a booking's name and merchant (lower case) to the
// category of its latest booking: Sure's recurring payments have none.
//
//	{Date: 09-05, Name: "Adobe", Category: "Software"} → {"adobe": "Software"}
func sureCategories(sure *sources.SureDataset) map[string]string {
	out := map[string]string{}
	latest := map[string]string{}
	for _, t := range sure.Transactions {
		if t.Category == "" {
			continue
		}
		for _, name := range []string{t.Name, t.Merchant} {
			key := strings.ToLower(strings.TrimSpace(name))
			if key == "" || t.Date < latest[key] {
				continue
			}
			latest[key], out[key] = t.Date, t.Category
		}
	}
	return out
}

func init() {
	Register(WidgetType{Key: "subscriptions", Decode: decodeSubs, Template: "widgets/subscriptions", Category: CategoryInsight,
		RefreshS: 3600, View: subsView,
		Queries: func(any) []Query { return []Query{peer(peerWallos, enums.ServiceWallos), surePeer} }})
}
