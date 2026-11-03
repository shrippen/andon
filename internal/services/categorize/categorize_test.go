package categorize_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"andon/internal/bayes"
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/categorize"
	"andon/internal/services/learn"
	"andon/internal/services/workbench"
	"andon/internal/testkit"
)

// The demo's studio supplies lack a category; their older booking says
// "Technik". The review names the filing, refuses a missing category,
// and the demo takes no writes.
func TestCategorize(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	testkit.Conn(t, d, who, space, enums.ServiceSure, "demo://sure")
	ctx := context.Background()

	page, err := categorize.Open(ctx, d, who, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var key string
	for _, c := range page.Cases {
		if c.CategoryID == "Technik" && c.Ticked {
			key = c.Key
		}
	}
	if key == "" {
		t.Fatalf("no proposed case: %+v", page.Cases)
	}

	steps := []workbench.Step{
		{Case: key, Action: categorize.ActFile, Fields: map[string]string{categorize.FieldCategory: "Technik"}},
		{Case: key, Action: categorize.ActFile, Fields: map[string]string{categorize.FieldCategory: "nope"}},
		{Case: "m-nope", Action: categorize.ActFile, Fields: map[string]string{categorize.FieldCategory: "Technik"}},
	}
	plan, err := categorize.Review(ctx, d, who, "", steps)
	if err != nil || plan.Writes() != 1 || plan.Steps[1].Problem != categorize.ProblemCategory || plan.Steps[2].Problem != categorize.ProblemGone {
		t.Fatalf("review: %+v %v", plan, err)
	}
	if _, err := categorize.Apply(ctx, d, who, "", steps[:1], ""); !errors.Is(err, categorize.ErrDemo) {
		t.Fatalf("demo write: %v", err)
	}
}

// A merchant without filed bookings gets the categories the stored
// model learned as a suggestion, nothing chosen nor ticked; the job
// teaches the model Sure's filed bookings.
func TestCategorizeLearned(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	conn, err := content.Connection(d, testkit.Conn(t, d, who, space, enums.ServiceSure, "demo://sure"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	teach := func(m *bayes.Model) bool {
		return m.Learn("x", "Hosting", []string{"w:bäckerei", "w:bakery"})
	}
	if err := learn.Learn(d, categorize.PurposeCategory, conn, model.UserHolder(who.UserID), teach); err != nil {
		t.Fatal(err)
	}
	page, err := categorize.Open(ctx, d, who, "")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var found bool
	for _, c := range page.Cases {
		if c.Of > 0 {
			continue
		}
		found = true
		if c.CategoryID != "" || c.Ticked || len(c.Suggest) == 0 || c.Suggest[0] != (metrics.Pick{Value: "Hosting", Label: "Hosting"}) {
			t.Errorf("learned case: %+v", c)
		}
	}
	if !found {
		t.Fatalf("no case without votes: %+v", page.Cases)
	}

	if err := learn.Train(ctx, d, time.Now()); err != nil {
		t.Fatal(err)
	}
	m := learn.Model(d, categorize.PurposeCategory, conn, model.UserHolder(who.UserID))
	if !m.Model.Knows("w:nordhost") {
		t.Errorf("not trained from Sure: %+v", m.Model.Stats())
	}
}
