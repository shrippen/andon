package euer_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/services/euer"
	"andon/internal/services/verbund"
	"andon/internal/services/workbench"
	"andon/internal/sources"
	"andon/internal/testkit"
)

// The demo's Verbund: its EÜR field is found by name; once saved, the
// bench proposes the line of each vendor's last expense (never one of
// the category), and a dropdown field takes only its own values.
func TestEuerBench(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	ninja := testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, "demo://invoiceninja")
	sure := testkit.Conn(t, d, who, space, enums.ServiceSure, "demo://sure")
	id, err := verbund.Create(d, who, "Studio", []int64{ninja, sure}, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, today := context.Background(), time.Now()

	view, err := verbund.CategoriesOf(ctx, d, who, id)
	if err != nil {
		t.Fatal(err)
	}
	if view.Slot != 3 || !view.CanSave || len(view.Lines()) == 0 {
		t.Fatalf("view: slot %d, save %v", view.Slot, view.CanSave)
	}
	page, err := euer.Open(ctx, d, who, "", today.Year(), today)
	if err != nil || page.Slot != 0 {
		t.Fatalf("before saving: slot %d %v", page.Slot, err)
	}

	if len(view.Sure) == 0 || len(view.Categories) == 0 {
		t.Fatalf("no Sure categories or Ninja categories: %+v", view.Sure)
	}
	sureMap := map[string]string{view.Sure[0].Name: view.Categories[0].Key, "Unknown": "no-such-key"}
	if err := verbund.SaveCategories(ctx, d, who, id, verbund.CategorySave{Sure: sureMap, Slot: view.Slot}, ""); err != nil {
		t.Fatal(err)
	}
	again, _ := verbund.CategoriesOf(ctx, d, who, id)
	if again.Sure[0].Mapped != view.Categories[0].Key {
		t.Errorf("Sure mapping not kept: %+v", again.Sure[0])
	}
	page, err = euer.Open(ctx, d, who, "", today.Year(), today)
	if err != nil || page.Slot != 3 || page.Label != "EÜR" || len(page.Cases) == 0 || page.Suggested == 0 || !page.Dropdown {
		t.Fatalf("after saving: %+v %v", page, err)
	}
	vendorLine := demoVendorLines(t, ctx)
	for _, c := range page.Cases {
		if c.Line != vendorLine[c.Expense.VendorKey] {
			t.Errorf("%s: line %q, vendor's %q", c.Expense.Key, c.Line, vendorLine[c.Expense.VendorKey])
		}
	}

	c := page.Cases[0]
	step := workbench.Step{Case: c.Expense.Key, Action: euer.ActSet, Fields: map[string]string{euer.FieldLine: page.Lines[0]}}
	odd := workbench.Step{Case: page.Cases[1].Expense.Key, Action: euer.ActSet, Fields: map[string]string{euer.FieldLine: "Quatsch"}}
	plan, err := euer.Review(ctx, d, who, "", today.Year(), []workbench.Step{step, {Case: "gone", Action: euer.ActSet}, odd})
	if err != nil || plan.Writes() != 1 || plan.Steps[1].Problem != euer.ProblemGone || plan.Steps[2].Problem != euer.ProblemValue {
		t.Fatalf("review: %+v %v", plan, err)
	}
	if _, err := euer.Apply(ctx, d, who, "", today.Year(), []workbench.Step{step}, ""); !errors.Is(err, euer.ErrDemo) {
		t.Fatalf("demo write: %v", err)
	}
}

// demoVendorLines reads the demo's line per vendor as the bench must.
func demoVendorLines(t *testing.T, ctx context.Context) map[string]string {
	t.Helper()
	src, err := sources.Get(sources.DataKey(enums.ServiceInvoiceNinja))
	if err != nil {
		t.Fatal(err)
	}
	n, err := src.Fetch(ctx, sources.Ctx{URL: "demo://invoiceninja"})
	if err != nil {
		t.Fatal(err)
	}
	return metrics.VendorEuer(n.(*sources.NinjaDataset), 3)
}
