package widgets

// "receipts_missing": business spending (Sure) with no receipt in
// Invoice Ninja, Paperless or the mailbox, largest first, each with a
// Paperless search to find or file it.

import (
	"math"
	"net/url"
	"sort"
	"strings"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// ReceiptsConfig is the "receipts_missing" widget's config.
type ReceiptsConfig struct {
	Days, Limit int
	MinAmount   float64 // overrides the rule's minimum, 0 = the rule's
}

const (
	cents              = 100
	defaultReceiptDays = 90
	defaultReceiptRows = 6
)

// ReceiptRow is one booking without receipt.
type ReceiptRow struct {
	Name, Date, Account string
	Amount              float64
	Search              string // Paperless search, "" without Paperless
}

func receiptsView(cfg ReceiptsConfig, results map[string]any, ctx ViewCtx) map[string]any {
	sure, ok := results["data"].(*sources.SureDataset)
	if !ok {
		return map[string]any{}
	}
	rule := ruleConfig("cross.expense_unrecorded", ctx.Settings)
	in := metrics.ReceiptInputs{Sure: sure, Accounts: asStringList(rule["accounts"]), MinAmount: floatOf(rule["min_amount"]),
		Window: int(floatOf(rule["date_window"])), Since: todayOf(ctx).AddDate(0, 0, -cfg.Days)}
	if cfg.MinAmount > 0 {
		in.MinAmount = cfg.MinAmount
	}
	in.Ninja, _ = results[peerNinja].(*sources.NinjaDataset)
	in.Mail, _ = results[peerMail].(*sources.MailDataset)
	paperless, _ := results[peerPaperless].(*sources.PaperlessDataset)
	in.Paperless = paperless

	var rows []ReceiptRow
	sum := 0.0
	for _, t := range metrics.MissingReceipts(in) {
		row := ReceiptRow{Name: t.Name, Date: t.Date, Account: t.Account, Amount: -t.Amount}
		if paperless != nil && paperless.URL != "" {
			row.Search = strings.TrimRight(paperless.URL, "/") + "/documents?query=" + url.QueryEscape(t.Name)
		}
		sum += row.Amount
		rows = append(rows, row)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Amount > rows[j].Amount })
	count := len(rows)
	if len(rows) > cfg.Limit {
		rows = rows[:cfg.Limit]
	}
	return map[string]any{"Rows": rows, "Count": count, "Sum": math.Round(sum*cents) / cents, "Currency": sure.Currency, "Setup": len(in.Accounts) == 0}
}

func init() {
	Tile[ReceiptsConfig]{Key: "receipts_missing", Detail: receiptsDetail, Category: CategoryInsight, Topic: TopicWork, Service: enums.ServiceSure, RefreshS: 3600,
		Fields: []Field{{Key: "days", Input: InputNumber, Default: defaultReceiptDays, Min: "7", Max: "365"},
			{Key: "limit", Input: InputNumber, Default: defaultReceiptRows, Min: "1", Max: "30"}, {Key: "min_amount", Input: InputNumber, Min: "0"}},
		Decode: func(r Raw) ReceiptsConfig {
			return ReceiptsConfig{Days: r.Int("days"), Limit: r.Int("limit"), MinAmount: r.Float("min_amount")}
		},
		View: receiptsView,
		Queries: func(ReceiptsConfig) []Query {
			return append(dataQuery(nil), peer(peerNinja, enums.ServiceInvoiceNinja), peer(peerPaperless, enums.ServicePaperless), peer(peerMail, enums.ServiceMail))
		},
		Calm: func(v map[string]any) bool { return v["Count"] == 0 && v["Setup"] == false }}.add()
}
