package sources

// Vikunja: open tasks with their due dates, and the ones done in the
// last days (for the check against Kimai). An API token with read access
// to projects and tasks is enough.
//
//	GET api/v1/projects                                  → [{id, title}]
//	GET api/v1/tasks/all?filter=done = false&per_page=…  → [{title, project_id, due_date, done, done_at, priority, labels[{title}]}]
//	GET api/v1/tasks/all?filter=done = true && done_at > now-7d

import (
	"context"
	"net/url"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

const (
	vikunjaPage     = "250"
	vikunjaDoneDays = 7
	// vikunjaNoDate is how Vikunja writes "no due date".
	vikunjaNoDate = "0001-01-01T00:00:00Z"
)

// VikunjaTask is one task.
type VikunjaTask struct {
	Title    string
	Project  string // its project's title
	Due      time.Time
	Done     bool
	DoneAt   time.Time
	Priority int
	Labels   []string
}

// VikunjaDataset is the open and recently done tasks.
type VikunjaDataset struct {
	URL   string
	Tasks []VikunjaTask
}

// Open lists the tasks not done.
func (d *VikunjaDataset) Open() []VikunjaTask {
	var out []VikunjaTask
	for _, t := range d.Tasks {
		if !t.Done {
			out = append(out, t)
		}
	}
	return out
}

var VikunjaData = source{key: "vikunja.data", ttl: opsTTL, service: enums.ServiceVikunja, fetch: fetchVikunja}

func fetchVikunja(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoVikunja(time.Now().UTC()), nil
	}
	secret, err := needSecret(sctx)
	if err != nil {
		return nil, err
	}
	api := services.BearerApi(sctx.URL, secret, sctx.TLS())
	projects := map[int64]string{}
	raw, err := api.Get(ctx, "api/v1/projects", url.Values{"per_page": {vikunjaPage}})
	if err != nil {
		return nil, fetchError(err)
	}
	for _, p := range asList(raw) {
		m := asMap(p)
		projects[asInt64(m["id"])] = asStr(m["title"])
	}

	data := &VikunjaDataset{URL: sctx.URL}
	since := time.Now().AddDate(0, 0, -vikunjaDoneDays).UTC().Format(time.RFC3339)
	for _, filter := range []string{"done = false", "done = true && done_at > " + since} {
		list, err := api.Get(ctx, "api/v1/tasks/all", url.Values{"filter": {filter}, "per_page": {vikunjaPage}})
		if err != nil {
			return nil, fetchError(err)
		}
		for _, item := range asList(list) {
			data.Tasks = append(data.Tasks, vikunjaTask(asMap(item), projects))
		}
	}
	return data, nil
}

func vikunjaTask(m map[string]any, projects map[int64]string) VikunjaTask {
	t := VikunjaTask{Title: asStr(m["title"]), Project: projects[asInt64(m["project_id"])], Priority: int(asFloat(m["priority"]))}
	t.Done, _ = m["done"].(bool)
	if due := asStr(m["due_date"]); due != vikunjaNoDate {
		t.Due = parseTime(due)
	}
	if done := asStr(m["done_at"]); done != vikunjaNoDate {
		t.DoneAt = parseTime(done)
	}
	for _, l := range asList(m["labels"]) {
		t.Labels = append(t.Labels, asStr(asMap(l)["title"]))
	}
	return t
}

func DemoVikunja(now time.Time) *VikunjaDataset {
	var p struct {
		URL      string
		Projects []struct {
			ID    int64
			Title string
		}
		Tasks []struct {
			VikunjaTask
			Project int64
			Due     string
		}
	}
	demoworld.MustDecode("todo", now, &p)
	names := map[int64]string{}
	for _, pr := range p.Projects {
		names[pr.ID] = pr.Title
	}
	data := &VikunjaDataset{URL: p.URL}
	for _, t := range p.Tasks {
		task := t.VikunjaTask
		task.Project = names[t.Project]
		task.Due, _ = time.Parse(time.DateOnly, t.Due)
		data.Tasks = append(data.Tasks, task)
	}
	return data
}

func init() {
	Register(VikunjaData)
	Register(testOf{VikunjaData, func(d any) map[string]any { return map[string]any{"open": len(d.(*VikunjaDataset).Open())} }})
}
