package vendors_test

import (
	"context"
	"errors"
	"testing"

	"andon/internal/enums"
	"andon/internal/services/vendors"
	"andon/internal/services/workbench"
	"andon/internal/testkit"
)

// The review names a merge of two vendors and refuses one into itself;
// the demo takes no writes.
func TestMerge(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, "demo://invoiceninja")
	ctx := context.Background()

	page, err := vendors.Open(ctx, d, who, "")
	if err != nil || len(page.Scopes) != 1 {
		t.Fatalf("open: %+v %v", page, err)
	}
	steps := []workbench.Step{
		{Case: "demo-v2", Action: vendors.ActMerge, Fields: map[string]string{vendors.FieldInto: "demo-v1"}},
		{Case: "demo-v1", Action: vendors.ActMerge, Fields: map[string]string{vendors.FieldInto: "demo-v1"}},
		{Case: "nope", Action: vendors.ActMerge, Fields: map[string]string{vendors.FieldInto: "demo-v1"}},
	}
	plan, err := vendors.Review(ctx, d, who, "", steps)
	if err != nil || plan.Writes() != 1 || plan.Steps[1].Problem != vendors.ProblemSelf || plan.Steps[2].Problem != vendors.ProblemGone {
		t.Fatalf("review: %+v %v", plan, err)
	}
	if plan.Steps[0].Item.From.Key != "demo-v2" || plan.Steps[0].Item.Into.Key != "demo-v1" {
		t.Errorf("merge: %+v", plan.Steps[0].Item)
	}
	if _, err := vendors.Apply(ctx, d, who, "", steps[:1], ""); !errors.Is(err, vendors.ErrDemo) {
		t.Fatalf("demo write: %v", err)
	}
}
