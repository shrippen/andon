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
type ReceiptsConfig struct{ Days, Limit int }

const (
	cents              = 100
	defaultReceiptDays = 90
	defaultReceiptRows = 6
)

func decodeReceipts(raw map[string]any) any {
	return ReceiptsConfig{Days: clampInt(asInt(raw["days"], defaultReceiptDays), 7, 365), Limit: clampInt(asInt(raw["limit"], defaultReceiptRows), 1, 30)}
}

// ReceiptRow is one booking without receipt.
type ReceiptRow struct {
	Name, Date, Account string
	Amount              float64
	Search              string // Paperless search, "" without Paperless
}

func receiptsView(cfgAny any, results map[string]any, ctx ViewCtx) map[string]any {
	cfg := cfgAny.(ReceiptsConfig)
	sure, ok := results["data"].(*sources.SureDataset)
	if !ok {
		return map[string]any{}
	}
	rule := ruleConfig("cross.expense_unrecorded", ctx.Settings)
	in := metrics.ReceiptInputs{Sure: sure, Accounts: asStringList(rule["accounts"]), MinAmount: floatOf(rule["min_amount"]),
		Window: int(floatOf(rule["date_window"])), Since: parseToday(ctx.Today).AddDate(0, 0, -cfg.Days)}
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
	Register(WidgetType{Key: "receipts_missing", Decode: decodeReceipts, Template: "widgets/receipts_missing", Category: CategoryInsight,
		Service: enums.ServiceSure, RefreshS: 3600, View: receiptsView,
		Queries: func(any) []Query {
			return append(dataQuery(nil), peer(peerNinja, enums.ServiceInvoiceNinja), peer(peerPaperless, enums.ServicePaperless), peer(peerMail, enums.ServiceMail))
		}})
}
