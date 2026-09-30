package rules

// Sure (personal finance) and its links to Invoice Ninja:
//
//	sure                 missed recurring payments, low balances, failed
//	                     bank sync, unusually large expenses, uncategorised
//	sure <-> invoiceninja open invoice already paid (income matches amount)
//	                     business expense missing in Invoice Ninja

import (
	"fmt"
	"sort"
	"strings"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const centTolerance = 0.01

func median(values []float64) float64 {
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	return sorted[len(sorted)/2]
}

func init() {
	Register("sure.recurring_missed", sureSvc, map[string]any{"days": 5.0}, on(recurringMissed))

	Register("sure.low_balance", sureSvc, map[string]any{"min_amount": 200.0}, on(lowBalance))

	Register("sure.sync_failed", sureSvc, nil, on(syncFailed))

	Register("sure.uncategorized", sureSvc, map[string]any{"min_count": 5.0, "lookback_days": 30.0}, on(uncategorized))

	// A recent expense well above what this payee usually costs, or large
	// without history: worth a second look (double booking, price rise).
	Register("sure.unusual_expense", sureSvc, map[string]any{"min_amount": 500.0, "factor": 3.0, "days": 14.0}, on(unusualExpense))

	registerSureCross()
}

// sureLink links a page of Sure.
func sureLink(data *sources.SureDataset, path string) string {
	return strings.TrimRight(data.URL, "/") + "/" + path
}

func recurringMissed(data *sources.SureDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, r := range data.Recurring {
		next, ok := metrics.ParseDay(r.Next)
		if r.Status != "active" || !ok || env.Today.Sub(next).Hours()/hoursPerDay <= cfgFloat(cfg, "days") {
			continue
		}
		found = append(found, svcFinding(sureSvc, "sure.recurring_missed", "missed:"+r.Name+":"+r.Next, "sure.missed",
			enums.SeverityWarn, sureLink(data, "recurring_transactions"), map[string]any{"name": r.Name, "amount": Money(r.Amount, data.Currency), "day": Day(next)}))
	}
	return found
}

func lowBalance(data *sources.SureDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, a := range data.Accounts {
		if a.Type != "depository" || a.Balance >= cfgFloat(cfg, "min_amount") {
			continue
		}
		level := enums.SeverityWarn
		if a.Balance < 0 {
			level = enums.SeverityCritical
		}
		found = append(found, svcFinding(sureSvc, "sure.low_balance", "low:"+a.ID, "sure.low", level,
			sureLink(data, "accounts/"+a.ID), map[string]any{"account": a.Name, "amount": Money(a.Balance, a.Currency)}))
	}
	return found
}

func syncFailed(data *sources.SureDataset, cfg map[string]any, env Env) []Finding {
	if data.SyncError == "" {
		return nil
	}
	return []Finding{svcFinding(sureSvc, "sure.sync_failed", "sync", "sure.sync", enums.SeverityWarn, data.URL,
		map[string]any{"name": data.SyncError})}
}

func uncategorized(data *sources.SureDataset, cfg map[string]any, env Env) []Finding {
	since := env.Today.AddDate(0, 0, -cfgInt(cfg, "lookback_days"))
	count := 0
	for _, t := range data.Transactions {
		d, ok := metrics.ParseDay(t.Date)
		if ok && !d.Before(since) && t.Category == "" {
			count++
		}
	}
	if count < cfgInt(cfg, "min_count") {
		return nil
	}
	return []Finding{svcFinding(sureSvc, "sure.uncategorized", "uncategorized", "sure.uncategorized", enums.SeverityInfo,
		sureLink(data, "transactions"), map[string]any{"count": count})}
}

func unusualExpense(data *sources.SureDataset, cfg map[string]any, env Env) []Finding {
	history := map[string][]float64{}
	for _, t := range data.Transactions {
		if t.Amount < 0 {
			history[strings.ToLower(t.Name)] = append(history[strings.ToLower(t.Name)], -t.Amount)
		}
	}
	since := env.Today.AddDate(0, 0, -cfgInt(cfg, "days"))
	var found []Finding
	for _, t := range data.Transactions {
		d, ok := metrics.ParseDay(t.Date)
		spent := -t.Amount
		if !ok || d.Before(since) || spent < cfgFloat(cfg, "min_amount") {
			continue
		}
		past := history[strings.ToLower(t.Name)]
		if len(past) > 2 && spent < median(past)*cfgFloat(cfg, "factor") {
			continue
		}
		found = append(found, svcFinding(sureSvc, "sure.unusual_expense", "unusual:"+t.ID, "sure.unusual", enums.SeverityInfo,
			sureLink(data, "transactions"), map[string]any{"name": t.Name, "amount": Money(spent, data.Currency), "day": Day(d)}))
	}
	return found
}

// sureNinjaSrc are the sources of bank vs. invoice findings.
var sureNinjaSrc = []string{string(enums.ServiceSure), string(enums.ServiceInvoiceNinja)}

func registerSureCross() {
	// An income in the bank that pays an open invoice (its number in the
	// booking text, else the same amount): probably not recorded yet.
	Register("cross.invoice_paid", Cross, map[string]any{"days": 90.0}, invoicePaid)

	// Money from a known client that pays no open invoice.
	Register("cross.payment_unmatched", Cross, map[string]any{"days": 30.0}, paymentUnmatched)

	// Business account spending without any receipt: no expense in
	// Invoice Ninja, no invoice document in Paperless, no invoice mail.
	// Only accounts named in "accounts" count: private spending stays out.
	Register("cross.expense_unrecorded", Cross, map[string]any{"accounts": []any{}, "min_amount": 20.0, "lookback_days": 60.0, "date_window": 10.0}, expenseUnrecorded)

	// Sure sees a recurring payment that looks like a subscription, but
	// Wallos (where subscriptions are kept) has no entry for it.
	Register("cross.wallos_missing", Cross, map[string]any{"max_monthly": 100.0, "ignore_names": []any{}}, wallosMissing)
}

func invoicePaid(_ any, cfg map[string]any, env Env) []Finding {
	sure, ok1 := env.Datasets[string(enums.ServiceSure)].(*sources.SureDataset)
	ninja, ok2 := env.Datasets[string(enums.ServiceInvoiceNinja)].(*sources.NinjaDataset)
	if !ok1 || !ok2 {
		return nil
	}
	var found []Finding
	for _, m := range metrics.PaymentMatches(sure, ninja, env.Today, cfgInt(cfg, "days")) {
		// An amount alone may be anybody's payment: no hint for it.
		if !m.Sure() {
			continue
		}
		found = append(found, Finding{
			Fingerprint: fmt.Sprintf("paid:%d", m.Invoice.ID), Severity: enums.SeverityWarn,
			Message: "cross.invoice_paid", Params: map[string]any{"number": m.Invoice.Number, "client": m.Invoice.Client,
				"amount": Money(m.Txn.Amount, ninja.Currency), "day": Day(m.Day), "account": m.Txn.Account},
			ActionURL: "/billing#payments", ActionLabel: "book_payment",
			Sources: sureNinjaSrc,
		})
	}
	return found
}

func paymentUnmatched(_ any, cfg map[string]any, env Env) []Finding {
	sure, ok1 := env.Datasets[string(enums.ServiceSure)].(*sources.SureDataset)
	ninja, ok2 := env.Datasets[string(enums.ServiceInvoiceNinja)].(*sources.NinjaDataset)
	if !ok1 || !ok2 {
		return nil
	}
	days := cfgInt(cfg, "days")
	matches := metrics.PaymentMatches(sure, ninja, env.Today, days)
	var found []Finding
	for _, t := range metrics.UnmatchedIncome(sure, ninja, matches, env.Today, days) {
		found = append(found, Finding{
			Fingerprint: "unmatched:" + t.ID, Severity: enums.SeverityInfo,
			Message: "cross.payment_unmatched", Params: map[string]any{"name": t.Name, "amount": Money(t.Amount, sure.Currency), "day": DayStr(t.Date)},
			Sources: sureNinjaSrc,
		})
	}
	return found
}

func expenseUnrecorded(_ any, cfg map[string]any, env Env) []Finding {
	sure, ok1 := env.Datasets[string(enums.ServiceSure)].(*sources.SureDataset)
	ninja, ok2 := env.Datasets[string(enums.ServiceInvoiceNinja)].(*sources.NinjaDataset)
	if !ok1 || !ok2 {
		return nil
	}
	in := ReceiptInputsOf(env, cfg)
	in.Since = env.Today.AddDate(0, 0, -cfgInt(cfg, "lookback_days"))
	var found []Finding
	for _, t := range metrics.MissingReceipts(in) {
		found = append(found, Finding{
			Fingerprint: "expense:" + t.ID, Severity: enums.SeverityInfo,
			Message: "cross.expense_unrecorded", Params: map[string]any{"name": t.Name, "amount": Money(-t.Amount, sure.Currency),
				"day": DayStr(t.Date), "account": t.Account},
			ActionURL: strings.TrimRight(ninja.URL, "/") + "/expenses/create", ActionLabel: "open_in_invoiceninja",
			Sources: sureNinjaSrc,
		})
	}
	return found
}

func wallosMissing(_ any, cfg map[string]any, env Env) []Finding {
	sure, ok1 := env.Datasets[string(enums.ServiceSure)].(*sources.SureDataset)
	wallos, ok2 := env.Datasets[string(enums.ServiceWallos)].(*sources.WallosDataset)
	if !ok1 || !ok2 {
		return nil
	}
	missing := metrics.NotInWallos(sure, wallos, cfgFloat(cfg, "max_monthly"), stringsSlice(cfg["ignore_names"]))
	if len(missing) == 0 {
		return nil
	}
	names := make([]string, len(missing))
	for i, r := range missing {
		names[i] = r.Name
	}
	return []Finding{{
		Fingerprint: "missing", Severity: enums.SeverityInfo, Message: "cross.wallos_missing",
		Params:    map[string]any{"count": len(missing), "names": shortList(names)},
		ActionURL: wallos.URL, ActionLabel: "open_in_wallos",
		Sources: []string{string(enums.ServiceSure), string(enums.ServiceWallos)},
	}}
}

// ReceiptInputsOf collects the receipt sources of a scope with the
// cross.expense_unrecorded settings; Since is left to the caller.
func ReceiptInputsOf(env Env, cfg map[string]any) metrics.ReceiptInputs {
	in := metrics.ReceiptInputs{Accounts: stringsSlice(cfg["accounts"]), MinAmount: cfgFloat(cfg, "min_amount"), Window: cfgInt(cfg, "date_window")}
	in.Sure, _ = env.Datasets[string(enums.ServiceSure)].(*sources.SureDataset)
	in.Ninja, _ = env.Datasets[string(enums.ServiceInvoiceNinja)].(*sources.NinjaDataset)
	in.Paperless, _ = env.Datasets[string(enums.ServicePaperless)].(*sources.PaperlessDataset)
	in.Mail, _ = env.Datasets[string(enums.ServiceMail)].(*sources.MailDataset)
	return in
}
