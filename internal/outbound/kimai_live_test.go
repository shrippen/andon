package outbound_test

import (
	"context"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"testing"
	"time"

	"andon/internal/drivers/services"
	"andon/internal/enums"
	"andon/internal/outbound"
	"andon/internal/testkit/live"
)

// Kinds of Kimai test entries in the write log.
const (
	kindSheet = "timesheet"
	kindTag   = "tag"
)

// kimaiTime is how a sheet's begin and end are sent: local time.
const kimaiTime = "2006-01-02T15:04:05"

// A finished timesheet booked yesterday with a new tag, then edited,
// described and deleted: the calls of the time tracker.
func TestKimaiSheetsLive(t *testing.T) {
	kimai := live.Target(t, live.Kimai)
	api := kimaiAPI(kimai)
	ctx := context.Background()
	name := live.Name(t)
	project, activity := kimaiPair(t, api)

	// A tag of its own, so the sheet's tags can be checked.
	outbound.KimaiTags(ctx, kimai, []string{name})
	tagID := kimaiTag(t, api, name)
	live.Created(t, live.Kimai, kindTag, tagID)
	t.Cleanup(func() { kimaiRemove(t, api, kindTag, "tags/", tagID) })

	begin := time.Now().AddDate(0, 0, -1).Truncate(time.Hour)
	sheet := outbound.KimaiSheet{
		Project: project, Activity: activity, Description: name, Tags: []string{name},
		Begin: begin.Format(kimaiTime), End: begin.Add(30 * time.Minute).Format(kimaiTime),
		Billable: enums.BillableNo,
	}
	if err := outbound.KimaiCreate(ctx, kimai, sheet); err != nil {
		t.Fatalf("create: %v", err)
	}
	id := kimaiSheet(t, api, "timesheets", url.Values{"term": {name}, "full": {"true"}}, name)
	live.Created(t, live.Kimai, kindSheet, id)
	t.Cleanup(func() { kimaiDelete(t, kimai, id) })

	got := kimaiRead(t, api, id)
	if !slices.Contains(kimaiTagNames(got), name) {
		t.Errorf("tags after create = %v, want %q", got["tags"], name)
	}

	// Edit: longer, tags kept.
	sheet.End = begin.Add(45 * time.Minute).Format(kimaiTime)
	live.Change(t, live.Kimai, live.Update, kindSheet, id)
	if err := outbound.KimaiEdit(ctx, kimai, id, sheet); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if d, _ := kimaiRead(t, api, id)["duration"].(float64); d != (45 * time.Minute).Seconds() {
		t.Errorf("duration after edit = %v s, want 2700", d)
	}

	described := name + " described"
	live.Change(t, live.Kimai, live.Update, kindSheet, id)
	if err := outbound.KimaiDescribe(ctx, kimai, id, described); err != nil {
		t.Fatalf("describe: %v", err)
	}
	if d := kimaiRead(t, api, id)["description"]; d != described {
		t.Errorf("description = %v, want %q", d, described)
	}
}

// A timer started and stopped. Kimai may stop running timers when one
// starts, so the test only runs while none runs.
func TestKimaiTimerLive(t *testing.T) {
	kimai := live.Target(t, live.Kimai)
	api := kimaiAPI(kimai)
	ctx := context.Background()
	name := live.Name(t)
	project, activity := kimaiPair(t, api)

	active, err := api.Get(ctx, "timesheets/active", nil)
	if err != nil {
		t.Fatalf("active: %v", err)
	}
	if list, _ := active.([]any); len(list) > 0 {
		t.Skip("a timer runs: starting one could stop it")
	}

	if err := outbound.KimaiStart(ctx, kimai, project, activity, name); err != nil {
		t.Fatalf("start: %v", err)
	}
	id := kimaiSheet(t, api, "timesheets/active", nil, name)
	live.Created(t, live.Kimai, kindSheet, id)
	t.Cleanup(func() { kimaiDelete(t, kimai, id) })

	// Kimai refuses to stop a timer of zero duration: start it earlier
	// (an edit without End keeps it running).
	begin := time.Now().Add(-5 * time.Minute).Format(kimaiTime)
	live.Change(t, live.Kimai, live.Update, kindSheet, id)
	if err := outbound.KimaiEdit(ctx, kimai, id, outbound.KimaiSheet{Begin: begin, Description: name}); err != nil {
		t.Fatalf("edit running: %v", err)
	}
	if end := kimaiRead(t, api, id)["end"]; end != nil {
		t.Fatalf("timer stopped by edit, end = %v", end)
	}

	live.Change(t, live.Kimai, live.Update, kindSheet, id)
	if err := outbound.KimaiStop(ctx, kimai, id); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if end := kimaiRead(t, api, id)["end"]; end == nil {
		t.Error("timer still runs after stop")
	}
}

func kimaiAPI(to outbound.Target) services.KimaiApi {
	return services.KimaiApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
}

// kimaiPair picks a visible project and one of its activities.
func kimaiPair(t *testing.T, api services.KimaiApi) (int64, int64) {
	t.Helper()
	ctx := context.Background()
	projects, err := api.Get(ctx, "projects", url.Values{"visible": {"1"}})
	if err != nil {
		t.Fatalf("projects: %v", err)
	}
	for _, p := range rows(projects) {
		project := num(p["id"])
		activities, err := api.Get(ctx, "activities", url.Values{"visible": {"1"}, "project": {strconv.FormatInt(project, 10)}})
		if err != nil {
			t.Fatalf("activities: %v", err)
		}
		if list := rows(activities); len(list) > 0 {
			return project, num(list[0]["id"])
		}
	}
	t.Skip("no project with an activity")
	return 0, 0
}

// kimaiTag finds the id of the tag called name.
func kimaiTag(t *testing.T, api services.KimaiApi, name string) int64 {
	t.Helper()
	found, err := api.Get(context.Background(), "tags/find", url.Values{"name": {name}})
	if err != nil {
		t.Fatalf("find tag: %v", err)
	}
	for _, tag := range rows(found) {
		if tag["name"] == name {
			return num(tag["id"])
		}
	}
	t.Fatalf("tag %q not created", name)
	return 0
}

// kimaiSheet finds the sheet described as name in a list, e.g.
// "timesheets/active"; the create calls answer without its id.
func kimaiSheet(t *testing.T, api services.KimaiApi, from string, params url.Values, name string) int64 {
	t.Helper()
	list, err := api.Get(context.Background(), from, params)
	if err != nil {
		t.Fatalf("list %s: %v", from, err)
	}
	for _, s := range rows(list) {
		if s["description"] == name {
			return num(s["id"])
		}
	}
	t.Fatalf("sheet %q not found in %s", name, from)
	return 0
}

func kimaiRead(t *testing.T, api services.KimaiApi, id int64) map[string]any {
	t.Helper()
	raw, err := api.Get(context.Background(), "timesheets/"+strconv.FormatInt(id, 10), nil)
	if err != nil {
		t.Fatalf("read sheet %d: %v", id, err)
	}
	m, _ := raw.(map[string]any)
	return m
}

// kimaiTagNames reads a sheet's tags: names, or objects with a name.
func kimaiTagNames(sheet map[string]any) []string {
	list, _ := sheet["tags"].([]any)
	var names []string
	for _, tag := range list {
		switch v := tag.(type) {
		case string:
			names = append(names, v)
		case map[string]any:
			n, _ := v["name"].(string)
			names = append(names, n)
		}
	}
	return names
}

// kimaiDelete removes the test's sheet through the call under test.
func kimaiDelete(t *testing.T, to outbound.Target, id int64) {
	live.Change(t, live.Kimai, live.Delete, kindSheet, id)
	if err := outbound.KimaiDelete(context.Background(), to, id); err != nil {
		t.Errorf("delete sheet %d: %v", id, err)
	}
}

// kimaiRemove deletes a test entry Andon itself never deletes.
func kimaiRemove(t *testing.T, api services.KimaiApi, kind, path string, id int64) {
	live.Change(t, live.Kimai, live.Delete, kind, id)
	if _, err := api.Send(context.Background(), http.MethodDelete, path+strconv.FormatInt(id, 10), nil); err != nil {
		t.Errorf("delete %s %d: %v", kind, id, err)
	}
}

// rows reads a list answer, bare or under "data" / "results".
func rows(raw any) []map[string]any {
	list, _ := raw.([]any)
	if m, ok := raw.(map[string]any); ok {
		list, _ = m["data"].([]any)
		if list == nil {
			list, _ = m["results"].([]any)
		}
	}
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// num reads a JSON id: number or numeric string.
func num(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case string:
		id, _ := strconv.ParseInt(n, 10, 64)
		return id
	}
	return 0
}
