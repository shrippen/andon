package euer_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/services/euer"
	"andon/internal/services/verbund"
	"andon/internal/services/workbench"
	"andon/internal/testkit"
)

// The demo's Verbund: its EÜR field is found by name, categories get
// their lines from what the expenses carry; once saved, the bench
// proposes them for empty fields and the demo refuses the write.
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
	if view.Slot != 3 || len(view.Euer) == 0 || !view.CanSave || len(view.Lines()) == 0 {
		t.Fatalf("view: slot %d, rows %d, save %v", view.Slot, len(view.Euer), view.CanSave)
	}
	page, err := euer.Open(ctx, d, who, "", today.Year(), today)
	if err != nil || page.Slot != 0 {
		t.Fatalf("before saving: slot %d %v", page.Slot, err)
	}

	lines := map[string]string{}
	for _, r := range view.Euer {
		lines[r.Key] = r.Suggested
	}
	if len(view.Sure) == 0 || len(view.Categories) == 0 {
		t.Fatalf("no Sure categories or Ninja categories: %+v", view.Sure)
	}
	sureMap := map[string]string{view.Sure[0].Name: view.Categories[0].Key, "Unknown": "no-such-key"}
	if err := verbund.SaveCategories(ctx, d, who, id, verbund.CategorySave{Sure: sureMap, Slot: view.Slot, Euer: lines}, ""); err != nil {
		t.Fatal(err)
	}
	again, _ := verbund.CategoriesOf(ctx, d, who, id)
	if again.Sure[0].Mapped != view.Categories[0].Key {
		t.Errorf("Sure mapping not kept: %+v", again.Sure[0])
	}
	page, err = euer.Open(ctx, d, who, "", today.Year(), today)
	if err != nil || page.Slot != 3 || page.Label != "EÜR" || len(page.Cases) == 0 || page.Mapped == 0 {
		t.Fatalf("after saving: %+v %v", page, err)
	}

	c := page.Cases[0]
	step := workbench.Step{Case: c.Expense.Key, Action: euer.ActSet, Fields: map[string]string{euer.FieldLine: "Arbeitsmittel"}}
	plan, err := euer.Review(ctx, d, who, "", today.Year(), []workbench.Step{step, {Case: "gone", Action: euer.ActSet}})
	if err != nil || plan.Writes() != 1 || plan.Steps[1].Problem != euer.ProblemGone {
		t.Fatalf("review: %+v %v", plan, err)
	}
	if _, err := euer.Apply(ctx, d, who, "", today.Year(), []workbench.Step{step}, ""); !errors.Is(err, euer.ErrDemo) {
		t.Fatalf("demo write: %v", err)
	}
}
