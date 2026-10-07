package rules

// Tasks (Vikunja):
//
//	vikunja.overdue       open tasks past their due date
//	vikunja.due           open tasks due within "days", one hint each with
//	                      its date, so digest and calendar list them
//	cross.task_unbooked   a task of a Kimai project was done, Kimai has no
//	                      time on that project since a few days before

import (
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

var vikunjaSvc = string(enums.ServiceVikunja)

// bookSlack is how long before a task's end time may have been booked.
const bookSlack = 3 * 24 * time.Hour

func init() {
	Register("vikunja.overdue", vikunjaSvc, nil, on(vikunjaOverdue))
	Register("vikunja.due", vikunjaSvc, map[string]any{"days": 3.0}, on(vikunjaDue))
	Register("cross.task_unbooked", Cross, nil, taskUnbooked)
}

func vikunjaOverdue(data *sources.VikunjaDataset, _ map[string]any, env Env) []Finding {
	var titles []string
	for _, t := range data.Open() {
		if !t.Due.IsZero() && t.Due.Before(env.Today) {
			titles = append(titles, t.Title)
		}
	}
	if len(titles) == 0 {
		return nil
	}
	return []Finding{svcFinding(vikunjaSvc, "vikunja.overdue", "overdue", "vikunja.overdue", enums.SeverityWarn, data.URL,
		map[string]any{"count": len(titles), "titles": shortList(titles)})}
}

func vikunjaDue(data *sources.VikunjaDataset, cfg map[string]any, env Env) []Finding {
	until := env.Today.AddDate(0, 0, int(cfgFloat(cfg, "days")))
	var found []Finding
	for _, t := range data.Open() {
		if t.Due.IsZero() || t.Due.Before(env.Today) || t.Due.After(until) {
			continue
		}
		f := svcFinding(vikunjaSvc, "vikunja.due", "due:"+t.Project+"/"+t.Title, "vikunja.due", enums.SeverityInfo, data.URL,
			map[string]any{"title": t.Title, "project": orDash(t.Project), "day": Day(t.Due)})
		f.Due = t.Due.Format(time.DateOnly)
		found = append(found, f)
	}
	return found
}

func taskUnbooked(_ any, _ map[string]any, env Env) []Finding {
	vik, ok := env.Datasets[vikunjaSvc].(*sources.VikunjaDataset)
	kimai, ok2 := env.Datasets[string(enums.ServiceKimai)].(*sources.KimaiDataset)
	if !ok || !ok2 {
		return nil
	}
	var found []Finding
	for _, t := range vik.Tasks {
		if !t.Done || t.DoneAt.IsZero() {
			continue
		}
		project, ok := kimaiProjectOf(kimai, append([]string{t.Project}, t.Labels...))
		if !ok || bookedSince(kimai, project.ID, t.DoneAt.Add(-bookSlack)) {
			continue
		}
		found = append(found, Finding{Fingerprint: project.Name + "/" + t.Title, Severity: enums.SeverityInfo, Message: "cross.task_unbooked",
			Params:  map[string]any{"task": t.Title, "project": project.Name, "day": Day(t.DoneAt)},
			Sources: []string{vikunjaSvc, string(enums.ServiceKimai)}})
	}
	return found
}

// kimaiProjectOf finds the Kimai project a task's project or labels name.
func kimaiProjectOf(k *sources.KimaiDataset, names []string) (sources.KimaiProject, bool) {
	for _, n := range names {
		n = strings.ToLower(strings.TrimSpace(n))
		if n == "" {
			continue
		}
		for _, p := range k.Projects {
			if strings.EqualFold(p.Name, n) || strings.Contains(strings.ToLower(p.Name), n) || strings.Contains(n, strings.ToLower(p.Name)) {
				return p, true
			}
		}
	}
	return sources.KimaiProject{}, false
}

// bookedSince reports whether Kimai has time on the project since t.
func bookedSince(k *sources.KimaiDataset, projectID int64, t time.Time) bool {
	for _, s := range k.Timesheets {
		if s.ProjectID != projectID {
			continue
		}
		if day, ok := metrics.ParseDay(s.Begin); ok && !day.Before(metrics.Today(t)) {
			return true
		}
	}
	return false
}
