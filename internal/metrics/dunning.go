package metrics

// Dunning: which overdue invoice gets which reminder now, by Invoice
// Ninja's own schedule (company settings → reminders):
//
//	stage k enabled, Days, Schedule ─► its date: invoice date + Days,
//	                                   due date − Days or due date + Days
//	next enabled stage after those sent, its date reached ─► due now
//
// Without any reminder set up in Invoice Ninja, Andon's defaults apply
// (first DunningFirst days after the due date, then every DunningGap)
// and the page says so.

import (
	"time"

	"andon/internal/sources"
)

// Dunning defaults, in days, when Invoice Ninja schedules no reminder.
const (
	DunningFirst = 7
	DunningGap   = 14
)

// DunningCase is an invoice due for its next reminder.
type DunningCase struct {
	NinjaOpenInvoice
	Next  int       // the reminder to send (1–3)
	Since time.Time // the day it became due
}

// DunningPlan is what the cases follow.
type DunningPlan struct {
	Reminders [sources.ReminderStages]sources.NinjaReminder
	Defaults  bool // Invoice Ninja schedules none: Andon's defaults
}

// DunningPlanOf reads Invoice Ninja's schedule, else the defaults.
func DunningPlanOf(n *sources.NinjaDataset) DunningPlan {
	p := DunningPlan{Reminders: n.Reminders}
	for _, r := range p.Reminders {
		if r.Enabled {
			return p
		}
	}
	p.Defaults = true
	for i := range p.Reminders {
		p.Reminders[i] = sources.NinjaReminder{Enabled: true, Days: DunningFirst + i*DunningGap, Schedule: sources.AfterDueDate}
	}
	return p
}

// dateOf is when stage r of an invoice falls due; ok false without the
// date it counts from.
func dateOf(r sources.NinjaReminder, inv sources.NinjaInvoice) (time.Time, bool) {
	base := inv.DueDate
	if r.Schedule == sources.AfterInvoiceDate {
		base = inv.Date
	}
	d, ok := ParseDay(base)
	if !ok {
		return time.Time{}, false
	}
	if r.Schedule == sources.BeforeDueDate {
		return d.AddDate(0, 0, -r.Days), true
	}
	return d.AddDate(0, 0, r.Days), true
}

// DunningCases lists the open invoices whose next enabled reminder is
// due, most overdue first.
func DunningCases(n *sources.NinjaDataset, today time.Time) []DunningCase {
	if n == nil {
		return nil
	}
	plan := DunningPlanOf(n)
	var out []DunningCase
	for _, inv := range NinjaOpenInvoices(n, today) {
		for k := inv.Stage; k < sources.ReminderStages; k++ {
			r := plan.Reminders[k]
			if !r.Enabled {
				continue
			}
			due, ok := dateOf(r, inv.NinjaInvoice)
			if ok && !today.Before(due) {
				out = append(out, DunningCase{NinjaOpenInvoice: inv, Next: k + 1, Since: due})
			}
			break
		}
	}
	return out
}
