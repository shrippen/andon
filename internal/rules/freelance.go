package rules

import (
	"fmt"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

func init() {
	// A client paying notably slower than usual is an early cash warning.
	Register("in.payment_worse", ninjaSvc, map[string]any{"days": 10.0}, on(paymentWorse))
}

func paymentWorse(data *sources.NinjaDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, m := range metrics.PaymentMorale(data, cfgInt(cfg, "days"), metrics.CenterOf(env.Settings)) {
		if !m.Worse {
			continue
		}
		found = append(found, Finding{Fingerprint: fmt.Sprintf("slower:%d", m.ClientID), Severity: enums.SeverityWarn, Message: "in.payment_worse",
			Params:    map[string]any{"client": m.Client, "recent": m.RecentDays, "avg": m.UsualDays},
			ActionURL: strings.TrimRight(data.URL, "/") + "/#/clients", ActionLabel: "open_in_invoiceninja", Sources: []string{ninjaSvc}})
	}
	return found
}

func init() {
	// Cancellation deadlines of contracts filed in Paperless.
	Register("paperless.contract_notice", paperlessSvc, map[string]any{"info_days": 90.0, "warn_days": 45.0, "critical_days": 14.0}, on(contractNotice))
}

func contractNotice(data *sources.PaperlessDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, c := range data.Contracts {
		if c.Deadline.IsZero() || c.Deadline.Before(env.Today) {
			continue
		}
		days := int(c.Deadline.Sub(env.Today).Hours() / hoursPerDay)
		level, ok := expiryLevel(days, cfg)
		if !ok {
			continue
		}
		found = append(found, Finding{Fingerprint: fmt.Sprintf("contract:%d:%s", c.ID, c.Deadline.Format(time.DateOnly)),
			Severity: level, Message: "paperless.contract_notice",
			Params: map[string]any{"title": c.Title, "correspondent": c.Correspondent, "day": Day(c.Deadline), "days": days,
				"end": Day(c.End)},
			Due:       c.Deadline.Format(time.DateOnly),
			ActionURL: strings.TrimRight(data.URL, "/") + fmt.Sprintf("/documents/%d/details", c.ID), ActionLabel: "open_in_paperless",
			Sources: []string{paperlessSvc}})
	}
	return found
}
