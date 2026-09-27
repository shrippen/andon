package widgets

// "payment_days": per client, the days from invoice to payment as dots on
// one scale, the typical value (mean or median, space setting) as a mark
// and the payment target as a line.
//
//	Muster    ·· ·       |         ·     10 T
//	Beispiel        · ·|·  ·              25 T

import (
	"slices"
	"sort"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// PaymentDaysConfig is the "payment_days" widget's config.
type PaymentDaysConfig struct {
	Target, Limit int
	Months        int      // only invoices of the last months, 0 = all
	HideClients   []string // lower case
}

const (
	defaultPayTarget = 30
	defaultPayRows   = 6
	payScaleRoom     = 1.1 // the rightmost dot keeps a little room
)

func decodePaymentDays(raw map[string]any) any {
	return PaymentDaysConfig{Target: clampInt(asInt(raw["target"], defaultPayTarget), 1, 365),
		Limit: clampInt(asInt(raw["limit"], defaultPayRows), 1, 20), Months: clampInt(asInt(raw["months"], 0), 0, 120),
		HideClients: lowerList(raw["hide_clients"])}
}

// PayRow is one client: dot and mark positions in percent of the scale.
type PayRow struct {
	Client            string
	Typical, Count    int
	Dots              []float64
	TypicalX, TargetX float64
	Late              bool // typical beyond the target
}

func paymentDaysView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg := cfgAny.(PaymentDaysConfig)
	data, ok := results["data"].(*sources.NinjaDataset)
	if !ok {
		return map[string]any{}
	}
	center := metrics.CenterOf(ctx.Settings)
	var since time.Time
	if cfg.Months > 0 {
		since = parseToday(ctx.Today).AddDate(0, -cfg.Months, 0)
	}
	gaps := metrics.NinjaPaymentGapsSince(data, since)
	for id := range gaps {
		if slices.Contains(cfg.HideClients, strings.ToLower(metrics.NinjaClientName(data, id))) {
			delete(gaps, id)
		}
	}

	top := float64(cfg.Target)
	for _, list := range gaps {
		for _, g := range list {
			top = max(top, float64(g))
		}
	}
	top *= payScaleRoom
	x := func(days float64) float64 { return days / top * pctFull }

	var rows []PayRow
	for id, list := range gaps {
		row := PayRow{Client: metrics.NinjaClientName(data, id), Typical: center.TypicalDays(list), Count: len(list), TargetX: x(float64(cfg.Target))}
		for _, g := range list {
			row.Dots = append(row.Dots, x(float64(g)))
		}
		row.TypicalX, row.Late = x(float64(row.Typical)), row.Typical > cfg.Target
		rows = append(rows, row)
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Typical > rows[j].Typical })
	if len(rows) > cfg.Limit {
		rows = rows[:cfg.Limit]
	}
	return map[string]any{"Rows": rows, "Center": string(center), "Target": cfg.Target}
}

func init() {
	Register(WidgetType{Key: "payment_days", Decode: decodePaymentDays, Template: "widgets/payment_days", Category: CategoryInsight,
		Service: enums.ServiceInvoiceNinja, RefreshS: 3600, Queries: dataQuery, View: paymentDaysView})
}
