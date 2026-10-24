package rules

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

const (
	snipeSource = "snipeit"
	snipeOpen   = "open_in_snipeit"
)

func snipeURL(data *sources.SnipeDataset, path string) string {
	return strings.TrimRight(data.URL, "/") + "/" + path
}

func init() {
	// Lent gear past its agreed return date: remind before it is lost.
	Register("snipe.checkin_overdue", string(enums.ServiceSnipeIT), nil, on(checkinOverdue))

	Register("snipe.eol_reached", string(enums.ServiceSnipeIT), nil, on(eolReached))

	Register("snipe.license_expiring", string(enums.ServiceSnipeIT), map[string]any{"days": 30.0}, on(licenseExpiring))

	Register("snipe.license_seats", string(enums.ServiceSnipeIT), nil, on(licenseSeats))

	Register("snipe.audit_overdue", string(enums.ServiceSnipeIT), nil, on(auditOverdue))

	Register("snipe.consumable_low", string(enums.ServiceSnipeIT), nil, on(consumableLow))

	// One hint for all ready but unassigned devices: own gear lies in the
	// shelf as a matter of course, one hint per patch cable is noise.
	Register("snipe.unassigned_deployable", string(enums.ServiceSnipeIT), map[string]any{"days": 90.0}, on(unassignedDeployable))
}

func checkinOverdue(data *sources.SnipeDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, a := range data.Assets {
		due, ok := metrics.ParseDay(a.ExpectedCheckin)
		if !a.Assigned || !ok || !due.Before(env.Today) {
			continue
		}
		found = append(found, Finding{
			Fingerprint: fmt.Sprintf("checkin:%d", a.ID), Severity: enums.SeverityWarn, Message: "snipe.checkin_overdue",
			Params:    map[string]any{"asset": a.Name, "tag": a.Tag, "who": a.AssignedTo, "day": Day(due)},
			ActionURL: snipeURL(data, fmt.Sprintf("hardware/%d", a.ID)), ActionLabel: snipeOpen,
			Due: due.Format(time.DateOnly), Sources: []string{snipeSource},
		})
	}
	return found
}

func eolReached(data *sources.SnipeDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, a := range data.Assets {
		d, ok := metrics.ParseDay(a.EOLDate)
		if !ok || d.After(env.Today) {
			continue
		}
		found = append(found, Finding{
			Fingerprint: fmt.Sprintf("eol:%d", a.ID), Severity: enums.SeverityInfo, Message: "snipe.eol",
			Params:    map[string]any{"asset": a.Name, "tag": a.Tag, "day": DayStr(a.EOLDate)},
			ActionURL: snipeURL(data, fmt.Sprintf("hardware/%d", a.ID)), ActionLabel: snipeOpen,
			Sources: []string{snipeSource},
		})
	}
	return found
}

func licenseExpiring(data *sources.SnipeDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, l := range data.Licenses {
		d, ok := metrics.ParseDay(l.Expires)
		if !ok {
			continue
		}
		daysLeft := int(d.Sub(env.Today).Hours() / 24)
		if daysLeft < 0 || daysLeft > cfgInt(cfg, "days") {
			continue
		}
		found = append(found, Finding{
			Fingerprint: fmt.Sprintf("license:%d", l.ID), Severity: enums.SeverityWarn, Message: "snipe.license",
			Params:    map[string]any{"license": l.Name, "day": Day(d)},
			ActionURL: snipeURL(data, fmt.Sprintf("licenses/%d", l.ID)), ActionLabel: snipeOpen,
			Due: l.Expires, Sources: []string{snipeSource},
		})
	}
	return found
}

func licenseSeats(data *sources.SnipeDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, l := range data.Licenses {
		if l.Seats == 0 || l.Free != 0 {
			continue
		}
		found = append(found, Finding{
			Fingerprint: fmt.Sprintf("seats:%d", l.ID), Severity: enums.SeverityInfo, Message: "snipe.seats",
			Params:    map[string]any{"license": l.Name, "seats": l.Seats},
			ActionURL: snipeURL(data, fmt.Sprintf("licenses/%d", l.ID)), ActionLabel: snipeOpen,
			Sources: []string{snipeSource},
		})
	}
	return found
}

func auditOverdue(data *sources.SnipeDataset, cfg map[string]any, env Env) []Finding {
	names := map[int64]string{}
	for _, a := range data.Assets {
		names[a.ID] = a.Name
	}
	var found []Finding
	for _, aid := range data.AuditOverdue {
		name := names[aid]
		if name == "" {
			name = fmt.Sprintf("#%d", aid)
		}
		found = append(found, Finding{
			Fingerprint: fmt.Sprintf("audit:%d", aid), Severity: enums.SeverityWarn, Message: "snipe.audit",
			Params:    map[string]any{"asset": name},
			ActionURL: snipeURL(data, fmt.Sprintf("hardware/%d", aid)), ActionLabel: snipeOpen,
			Sources: []string{snipeSource},
		})
	}
	return found
}

func consumableLow(data *sources.SnipeDataset, cfg map[string]any, env Env) []Finding {
	var found []Finding
	for _, c := range data.Consumables {
		if c.Min == 0 || c.Remaining >= c.Min {
			continue
		}
		found = append(found, Finding{
			Fingerprint: fmt.Sprintf("stock:%d", c.ID), Severity: enums.SeverityInfo, Message: "snipe.stock",
			Params:    map[string]any{"item": c.Name, "left": c.Remaining, "min": c.Min},
			ActionURL: snipeURL(data, fmt.Sprintf("consumables/%d", c.ID)), ActionLabel: snipeOpen,
			Sources: []string{snipeSource},
		})
	}
	return found
}

func unassignedDeployable(data *sources.SnipeDataset, cfg map[string]any, env Env) []Finding {
	var idle []aged
	for _, a := range data.Assets {
		if !a.Deployable || a.Assigned {
			continue
		}
		d, ok := metrics.ParseDay(a.LastChange)
		if !ok {
			continue
		}
		age := int(env.Today.Sub(d).Hours() / 24)
		if age < cfgInt(cfg, "days") {
			continue
		}
		idle = append(idle, aged{a.Name, age})
	}
	if len(idle) == 0 {
		return nil
	}
	sort.SliceStable(idle, func(i, j int) bool { return idle[i].days > idle[j].days })
	return []Finding{{
		Fingerprint: "unused", Severity: enums.SeverityInfo, Message: "snipe.unused",
		Params: map[string]any{"count": len(idle), "days": cfgInt(cfg, "days"), "oldest": idle[0].days,
			"names": agedNames(idle)},
		ActionURL: snipeURL(data, "hardware?status=RTD"), ActionLabel: snipeOpen,
		Sources: []string{snipeSource},
	}}
}
