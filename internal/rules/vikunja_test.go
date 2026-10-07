package rules_test

import (
	"testing"
	"time"

	"andon/internal/sources"
)

// TestVikunjaRules: the demo's invoice is overdue, the review due
// tomorrow carries its date (so it reaches digest and calendar).
func TestVikunjaRules(t *testing.T) {
	now := time.Now().UTC()
	data := sources.DemoVikunja(now)
	env := todayEnv(nil)
	env.Today = now.Truncate(24 * time.Hour)
	if got := run(t, "vikunja.overdue", data, env); len(got) != 1 || got[0].Params["count"] != 1 {
		t.Fatalf("overdue: %+v", got)
	}
	due := run(t, "vikunja.due", data, env)
	if len(due) != 1 || due[0].Due == "" {
		t.Fatalf("due: %+v", due)
	}
}

// TestTaskUnbooked: a task of a Kimai project done two days ago, while
// Kimai has no time on that project since.
func TestTaskUnbooked(t *testing.T) {
	now := time.Now().UTC()
	vik := &sources.VikunjaDataset{Tasks: []sources.VikunjaTask{
		{Title: "Musikschnitt", Project: "Showreel 2026", Done: true, DoneAt: now.AddDate(0, 0, -2)},
		{Title: "Farbe", Project: "Harbour Lights", Done: true, DoneAt: now.AddDate(0, 0, -2)}}}
	kimai := &sources.KimaiDataset{Projects: []sources.KimaiProject{{ID: 1, Name: "Showreel 2026"}, {ID: 2, Name: "Harbour Lights"}},
		Timesheets: []sources.KimaiSheet{{ProjectID: 2, Begin: now.AddDate(0, 0, -2).Format(time.RFC3339), Minutes: 120}}}
	env := todayEnv(nil)
	env.Datasets = map[string]any{"vikunja": vik, "kimai": kimai}
	got := run(t, "cross.task_unbooked", nil, env)
	if len(got) != 1 || got[0].Params["task"] != "Musikschnitt" {
		t.Fatalf("unbooked: %+v", got)
	}
}
