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

// PayRow is one client: dot and mark positions in percent of the scale.
type PayRow struct {
	Client            string
	Typical, Count    int
	Dots              []float64
	TypicalX, TargetX float64
	Late              bool // typical beyond the target
}

func paymentDaysView(cfg PaymentDaysConfig, data *sources.NinjaDataset, ctx ViewCtx) map[string]any {
	center := metrics.CenterOf(ctx.Settings)
	var since time.Time
	if cfg.Months > 0 {
		since = todayOf(ctx).AddDate(0, -cfg.Months, 0)
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
	Tile[PaymentDaysConfig]{Key: "payment_days", Category: CategoryInsight, Topic: TopicWork, Service: enums.ServiceInvoiceNinja, RefreshS: 3600,
		Fields: []Field{{Key: "target_days", Input: InputNumber, Default: defaultPayTarget, Min: "1", Max: "365"},
			{Key: "limit", Input: InputNumber, Default: defaultPayRows, Min: "1", Max: "20"},
			{Key: "months", Input: InputNumber, Min: "0", Max: "120"}, {Key: "hide_clients", Input: InputList}},
		Renames: []rename{{from: "target", to: "target_days"}},
		Decode: func(r Raw) PaymentDaysConfig {
			return PaymentDaysConfig{Target: r.Int("target_days"), Limit: r.Int("limit"), Months: r.Int("months"), HideClients: r.Lower("hide_clients")}
		},
		Queries: ownData[PaymentDaysConfig], View: dataView(paymentDaysView)}.add()
}
