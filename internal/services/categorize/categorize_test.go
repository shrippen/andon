package categorize_test

import (
	"context"
	"errors"
	"testing"

	"andon/internal/enums"
	"andon/internal/services/categorize"
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
