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
	"slices"
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
	Hand  bool // ticked by hand
}

// CloseTicksPref is the user pref (and result slot) of steps ticked by
// hand: {"2026-08": ["vat"]}.
const CloseTicksPref = "close_ticks"

// CloseTicksOf reads the stored ticks, which come back from JSON as
// map[string]any of []any.
func CloseTicksOf(raw any) map[string][]string {
	out := map[string][]string{}
	switch m := raw.(type) {
	case map[string][]string:
		return m
	case map[string]any:
		for month, list := range m {
			items, _ := list.([]any)
			for _, item := range items {
				if s, ok := item.(string); ok {
					out[month] = append(out[month], s)
				}
			}
		}
	}
	return out
}

// closeSteps are the steps in order, each with its on/off field.
var closeSteps = []string{"hours", "drafts", "receipts", "inbox", "vat"}

// MonthCloseConfig is the "month_close" widget's config.
type MonthCloseConfig struct {
	Hide    map[string]bool
	Current bool // the running month instead of last month
	Manual  bool // steps can be ticked by hand
}

func decodeMonthClose(r Raw) MonthCloseConfig {
	cfg := MonthCloseConfig{Hide: map[string]bool{}, Current: r.Pick("month") == "current", Manual: r.Bool("manual")}
	for _, step := range closeSteps {
		if !r.Bool("close_" + step) {
			cfg.Hide[step] = true
		}
	}
	return cfg
}

func monthCloseQueries(MonthCloseConfig) []Query {
	return append([]Query{kimaiPeer, peer(peerNinja, enums.ServiceInvoiceNinja),
		peer(peerPaperless, enums.ServicePaperless), peer(peerMail, enums.ServiceMail)}, bankPeers...)
}

func monthCloseView(cfg MonthCloseConfig, results map[string]any, ctx ViewCtx) map[string]any {
	today := todayOf(ctx)
	start := metrics.AddMonths(today, -1)
	if cfg.Current {
		start = metrics.AddMonths(today, 0)
	}
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
	if sure, ok := bankOf(results); ok {
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
		steps = append(steps, CloseStep{Key: "receipts", Done: missing == 0 && len(in.Accounts) > 0, Count: missing, URL: "/spaces/settings?section=rules#rule-cross.expense_unrecorded"})
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

	month := start.Format("2006-01")
	ticked := CloseTicksOf(results[CloseTicksPref])[month]
	shown := steps[:0]
	done := 0
	for _, s := range steps {
		if cfg.Hide[s.Key] {
			continue
		}
		if cfg.Manual && !s.Done && slices.Contains(ticked, s.Key) {
			s.Done, s.Hand = true, true
		}
		if s.Done {
			done++
		}
		shown = append(shown, s)
	}
	return map[string]any{"Steps": shown, "Done": done, "Month": start.Format("01/2006"), "MonthKey": month, "Manual": cfg.Manual,
		"Pct": pctOf(float64(done), float64(max(len(shown), 1)))}
}

func init() {
	Tile[MonthCloseConfig]{Key: "month_close", Detail: monthCloseDetail, Category: CategoryInsight, Topic: TopicWork, RefreshS: 1800, Extra: ExtraCloseTicks,
		Fields: []Field{sel("month", "previous", "previous", "current"), {Key: "manual", Input: InputCheck},
			{Key: "close_hours", Input: InputCheck, Default: true}, {Key: "close_drafts", Input: InputCheck, Default: true},
			{Key: "close_receipts", Input: InputCheck, Default: true}, {Key: "close_inbox", Input: InputCheck, Default: true},
			{Key: "close_vat", Input: InputCheck, Default: true}},
		Decode: decodeMonthClose, Queries: monthCloseQueries, View: monthCloseView,
		Calm: func(v map[string]any) bool {
			steps, _ := v["Steps"].([]CloseStep)
			return len(steps) > 0 && v["Done"] == len(steps)
		}}.add()
}
