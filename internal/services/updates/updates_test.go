package updates_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/services/updates"
	"andon/internal/services/workbench"
	"andon/internal/testkit"
)

// The demo's updates (WUD and a release feed) reach the review; one
// not pinned is refused,
// and the demo Gitea takes no pull requests.
func TestUpdates(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	for _, s := range []enums.ServiceType{enums.ServiceGitea, enums.ServiceWUD, enums.ServiceFreshRSS} {
		testkit.Conn(t, d, who, space, s, "demo://"+string(s))
	}
	ctx := context.Background()

	page, err := updates.Open(ctx, d, who, "")
	if err != nil || !page.Finders || len(page.Cases) == 0 {
		t.Fatalf("open: %+v %v", page, err)
	}
	var pinned, loose string
	for _, c := range page.Cases {
		t.Logf("%s %s %s→%s pinned=%v stack=%v", c.Key, c.Image, c.Tag, c.NewTag, c.Pinned, c.Stack != nil)
		if c.Pinned && pinned == "" {
			pinned = c.Key
		}
		if !c.Pinned && loose == "" {
			loose = c.Key
		}
	}
	if pinned == "" {
		t.Fatal("no pinned case in the demo")
	}
	if !slices.ContainsFunc(page.Cases, func(c metrics.UpdateCase) bool { return c.Release != "" && c.NewTag == "0.30.9" }) {
		t.Error("no case from the release feed (dawarich 0.30.9)")
	}

	steps := []workbench.Step{{Case: pinned, Action: updates.ActPropose, Fields: map[string]string{updates.FieldTag: "9.9.9"}}}
	if loose != "" {
		steps = append(steps, workbench.Step{Case: loose, Action: updates.ActPropose, Fields: map[string]string{updates.FieldTag: "9.9.9"}})
	}
	plan, err := updates.Review(ctx, d, who, "", steps)
	if err != nil || plan.Writes() != 1 || (loose != "" && plan.Steps[1].Problem != updates.ProblemPinned) {
		t.Fatalf("review: %+v %v", plan, err)
	}
	if _, _, err := updates.Apply(ctx, d, who, "", steps[:1], ""); !errors.Is(err, updates.ErrDemo) {
		t.Fatalf("demo write: %v", err)
	}

	// What's Up Docker names no project on GitHub: no notes to sum up.
	for _, c := range page.Cases {
		if c.Release == "" {
			if err := updates.Summarize(ctx, d, who, "", c.Key); !errors.Is(err, updates.ErrNoNotes) {
				t.Errorf("summarize %s: %v", c.Key, err)
			}
			break
		}
	}
}
