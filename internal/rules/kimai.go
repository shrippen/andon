package rules

import (
	"fmt"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const (
	kimaiSource        = "kimai"
	kimaiOpen          = "open_in_kimai"
	monthCloseDays     = 5
	utilizationFromDay = 10
)

func kimaiURL(data *sources.KimaiDataset, path string) string {
	return strings.TrimRight(data.URL, "/") + "/" + path
}

func hoursParam(minutes float64) map[string]any {
	return Num(minutes/60, 1)
}

func init() {
	Register("kimai.timer_running_long", string(enums.ServiceKimai), map[string]any{"hours": 10.0}, on(timerRunningLong))

	Register("kimai.missing_day", string(enums.ServiceKimai), map[string]any{"lookback_days": 10.0}, on(missingDay))

	Register("kimai.unbilled_hours", string(enums.ServiceKimai),
		map[string]any{"warn_days": 30.0, "critical_days": 60.0}, on(unbilledHours))

	Register("kimai.budget_burn", string(enums.ServiceKimai), map[string]any{"warn": 0.8, "critical": 1.0}, on(budgetBurn))

	// At the last four weeks' pace the budget runs out before the project ends.
	Register("kimai.budget_pace", string(enums.ServiceKimai), map[string]any{"min_gap_days": 7.0}, on(budgetPace))

	// Target from the Kimai work contract only; no contract, no hint.
	Register("kimai.utilization_low", string(enums.ServiceKimai), map[string]any{"goal": 0.7}, on(utilizationLow))

	Register("kimai.overtime", string(enums.ServiceKimai), map[string]any{"max_week_hours": 45.0, "weeks": 2.0}, on(overtime))

	Register("kimai.monthly_close", string(enums.ServiceKimai), nil, on(monthlyClose))
}

func timerRunningLong(data *sources.KimaiDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, r := range metrics.KimaiRunningNow(data, time.Now().UTC()) {
		if float64(r.RunningMin) < cfgFloat(cfg, "hours")*60 {
			continue
		}
		found = append(found, Finding{
			Fingerprint: fmt.Sprintf("timer:%d", r.Sheet.ID), Severity: enums.SeverityWarn, Message: "kimai.timer_long",
			Params:    map[string]any{"hours": hoursParam(float64(r.RunningMin))},
			ActionURL: kimaiURL(data, "timesheet/"), ActionLabel: kimaiOpen, Sources: []string{kimaiSource},
		})
	}
	return found
}

func missingDay(data *sources.KimaiDataset, cfg map[string]any, env Env) []Finding {
	booked := map[time.Time]bool{}
	for _, s := range data.Timesheets {
		if d, ok := metrics.ParseDay(s.Begin); ok {
			booked[d] = true
		}
	}
	start := env.Today.AddDate(0, 0, -cfgInt(cfg, "lookback_days"))
	// Days the Kimai work contract expects work; none without one.
	days := metrics.KimaiWorkdays(data, start, env.Today.AddDate(0, 0, -1))

	var found []Finding
	for _, d := range days {
		if booked[d] {
			continue
		}
		found = append(found, Finding{
			Fingerprint: "missing:" + d.Format("2006-01-02"), Severity: enums.SeverityInfo, Message: "kimai.missing_day",
			Params: map[string]any{"day": Day(d)}, ActionURL: kimaiURL(data, "timesheet/"),
			ActionLabel: kimaiOpen, Sources: []string{kimaiSource},
		})
	}
	return found
}

func unbilledHours(data *sources.KimaiDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, g := range metrics.KimaiUnbilled(data, env.Today) {
		if g.AgeDays < cfgInt(cfg, "warn_days") {
			continue
		}
		level := enums.SeverityWarn
		if g.AgeDays >= cfgInt(cfg, "critical_days") {
			level = enums.SeverityCritical
		}
		found = append(found, Finding{
			Fingerprint: fmt.Sprintf("unbilled:%d", g.CustomerID), Severity: level, Message: "kimai.unbilled",
			Params: map[string]any{
				"hours": hoursParam(float64(g.Minutes)), "customer": g.Customer,
				"amount": Money(g.Amount, ""), "oldest": DayStr(g.Oldest), "days": cfgFloat(cfg, "warn_days"),
			},
			ActionURL: kimaiURL(data, "abrechnung"), ActionLabel: kimaiOpen,
			Sources: []string{kimaiSource, string(enums.ServiceInvoiceNinja)},
		})
	}
	return found
}

func budgetBurn(data *sources.KimaiDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, p := range data.Projects {
		ratio, ok := metrics.BudgetUse(p, data, env.Today)
		if !ok || ratio < cfgFloat(cfg, "warn") {
			continue
		}
		level := enums.SeverityWarn
		if ratio >= cfgFloat(cfg, "critical") {
			level = enums.SeverityCritical
		}
		found = append(found, Finding{
			Fingerprint: fmt.Sprintf("budget:%d", p.ID), Severity: level,
			Message:     "kimai.budget",
			Params:      map[string]any{"project": p.Name, "percent": Num(ratio*100, 0)},
			ActionURL:   kimaiURL(data, fmt.Sprintf("admin/project/%d/details", p.ID)),
			ActionLabel: kimaiOpen, Sources: []string{kimaiSource},
		})
	}
	return found
}

func budgetPace(data *sources.KimaiDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, f := range metrics.BudgetForecasts(data, env.Today) {
		if f.RunOut.IsZero() || f.End.IsZero() || !f.End.After(env.Today) || f.GapDays < cfgInt(cfg, "min_gap_days") {
			continue
		}
		found = append(found, Finding{
			Fingerprint: fmt.Sprintf("pace:%d", f.ProjectID), Severity: enums.SeverityWarn, Message: "kimai.runout",
			Params: map[string]any{"project": f.Project, "runout": Day(f.RunOut), "end": Day(f.End),
				"gap": f.GapDays, "percent": Num(f.Used*100, 0)},
			ActionURL:   kimaiURL(data, fmt.Sprintf("admin/project/%d/details", f.ProjectID)),
			ActionLabel: kimaiOpen, Sources: []string{kimaiSource},
		})
	}
	return found
}

func utilizationLow(data *sources.KimaiDataset, cfg map[string]any, env Env) []Finding {
	if env.Today.Day() < utilizationFromDay {
		return nil
	}
	stats := metrics.KimaiSummaryOf(data, env.Today)
	if stats.Utilization == nil || *stats.Utilization >= cfgFloat(cfg, "goal") {
		return nil
	}
	return []Finding{{
		Fingerprint: "utilization:" + env.Today.Format("2006-01"), Severity: enums.SeverityInfo, Message: "kimai.utilization",
		Params:  map[string]any{"percent": Num(*stats.Utilization*100, 0), "goal": Num(cfgFloat(cfg, "goal")*100, 0)},
		Sources: []string{kimaiSource},
	}}
}

func overtime(data *sources.KimaiDataset, cfg map[string]any, env Env) []Finding {
	perWeek := map[time.Time]int{}
	for _, s := range data.Timesheets {
		if d, ok := metrics.ParseDay(s.Begin); ok {
			perWeek[metrics.WeekStart(d)] += s.Minutes
		}
	}

	weeksBack := cfgInt(cfg, "weeks")
	current := metrics.WeekStart(env.Today)
	weeks := make([]time.Time, weeksBack)
	for i := 0; i < weeksBack; i++ {
		weeks[i] = current.AddDate(0, 0, -7*(i+1))
	}
	limit := cfgFloat(cfg, "max_week_hours") * 60
	for _, w := range weeks {
		if float64(perWeek[w]) <= limit {
			return nil
		}
	}
	var sum int
	for _, w := range weeks {
		sum += perWeek[w]
	}
	average := float64(sum) / float64(len(weeks))
	return []Finding{{
		Fingerprint: "overtime:" + weeks[0].Format("2006-01-02"), Severity: enums.SeverityInfo, Message: "kimai.overtime",
		Params: map[string]any{
			"hours": hoursParam(average), "weeks": weeksBack, "limit": cfgFloat(cfg, "max_week_hours"),
		},
		Sources: []string{kimaiSource},
	}}
}

func monthlyClose(data *sources.KimaiDataset, cfg map[string]any, env Env) []Finding {
	if env.Today.Day() > monthCloseDays {
		return nil
	}
	start := metrics.AddMonths(env.Today, -1)
	end := metrics.MonthStart(env.Today).AddDate(0, 0, -1)
	var openMin int
	for _, s := range data.Timesheets {
		d, ok := metrics.ParseDay(s.Begin)
		if s.Billable && !s.Exported && ok && !d.Before(start) && !d.After(end) {
			openMin += s.Minutes
		}
	}
	if openMin == 0 {
		return nil
	}
	return []Finding{{
		Fingerprint: "close:" + start.Format("2006-01"), Severity: enums.SeverityWarn, Message: "kimai.monthly_close",
		Params:    map[string]any{"month": start.Format("01/2006"), "hours": hoursParam(float64(openMin))},
		ActionURL: kimaiURL(data, "abrechnung"), ActionLabel: kimaiOpen, Sources: []string{kimaiSource},
	}}
}
