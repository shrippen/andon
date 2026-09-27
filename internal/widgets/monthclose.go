package widgets

// "month_close": the steps that close last month, each ticked from the
// data where it can be (one line per connected service):
//
//	■ Stunden abgerechnet        (Kimai: no unexported billable time)
//	□ 1 Rechnungsentwurf offen   (Invoice Ninja: no drafts)
//	□ 3 Ausgaben ohne Beleg      (Sure + Paperless/Ninja/Mail)
//	■ Paperless-Posteingang leer
//	□ USt-Voranmeldung bis 10.10.  (never ticked: Andon cannot see it)

import (
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// CloseStep is one step of closing the month.
type CloseStep struct {
	Key   string // hours, drafts, receipts, inbox, vat
	Done  bool
	Count int
	Hours float64
	Due   string // vat only
	URL   string
}

func monthCloseQueries(any) []Query {
	return []Query{kimaiPeer, peer(peerNinja, enums.ServiceInvoiceNinja), peer(peerSure, enums.ServiceSure),
		peer(peerPaperless, enums.ServicePaperless), peer(peerMail, enums.ServiceMail)}
}

func monthCloseView(_ any, results map[string]any, ctx ViewCtx) map[string]any {
	today := parseToday(ctx.Today)
	start := metrics.AddMonths(today, -1)
	end := metrics.AddMonths(start, 1).AddDate(0, 0, -1)
	inMonth := func(day string) bool {
		d, ok := metrics.ParseDay(day)
		return ok && !d.Before(start) && !d.After(end)
	}

	var steps []CloseStep
	if kimai, ok := results[peerKimai].(*sources.KimaiDataset); ok {
		minutes := 0
		for _, s := range kimai.Timesheets {
			if s.Billable && !s.Exported && inMonth(s.Begin) {
				minutes += s.Minutes
			}
		}
		steps = append(steps, CloseStep{Key: "hours", Done: minutes == 0, Hours: float64(minutes) / minutesPerHour, URL: kimai.URL})
	}
	ninja, _ := results[peerNinja].(*sources.NinjaDataset)
	if ninja != nil {
		drafts := 0
		for _, i := range ninja.Invoices {
			if i.Status == "draft" {
				drafts++
			}
		}
		steps = append(steps, CloseStep{Key: "drafts", Done: drafts == 0, Count: drafts, URL: strings.TrimRight(ninja.URL, "/") + "/invoices"})
	}
	if sure, ok := results[peerSure].(*sources.SureDataset); ok {
		cfg := ruleConfig("cross.expense_unrecorded", ctx.Settings)
		in := metrics.ReceiptInputs{Sure: sure, Ninja: ninja, Accounts: asStringList(cfg["accounts"]), MinAmount: floatOf(cfg["min_amount"]),
			Window: int(floatOf(cfg["date_window"])), Since: start}
		in.Paperless, _ = results[peerPaperless].(*sources.PaperlessDataset)
		in.Mail, _ = results[peerMail].(*sources.MailDataset)
		missing := 0
		for _, t := range metrics.MissingReceipts(in) {
			if inMonth(t.Date) {
				missing++
			}
		}
		steps = append(steps, CloseStep{Key: "receipts", Done: missing == 0 && len(in.Accounts) > 0, Count: missing, URL: "/spaces/settings#rule-cross.expense_unrecorded"})
	}
	if paperless, ok := results[peerPaperless].(*sources.PaperlessDataset); ok {
		steps = append(steps, CloseStep{Key: "inbox", Done: paperless.Inbox == 0, Count: paperless.Inbox,
			URL: strings.TrimRight(paperless.URL, "/") + "/documents?sort=added"})
	}
	if tax, ok := metrics.ParseTaxSettings(ctx.Settings); ok {
		if d, found := metrics.VATReturnFor(tax, end); found {
			steps = append(steps, CloseStep{Key: "vat", Due: d.Due.Format(time.DateOnly)})
		}
	}

	done := 0
	for _, s := range steps {
		if s.Done {
			done++
		}
	}
	return map[string]any{"Steps": steps, "Done": done, "Month": start.Format("01/2006"),
		"Pct": pctOf(float64(done), float64(max(len(steps), 1)))}
}

func init() {
	Register(WidgetType{Key: "month_close", Decode: decodeEmpty, Template: "widgets/month_close", Category: CategoryInsight,
		RefreshS: 1800, Queries: monthCloseQueries, View: monthCloseView})
}
