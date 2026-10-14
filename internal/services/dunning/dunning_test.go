package dunning_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/services/dunning"
	"andon/internal/services/workbench"
	"andon/internal/testkit"
)

// The demo has overdue invoices; a reminder of the right stage passes the
// review, another stage does not, and the demo sends nothing.
func TestDunning(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, "demo://invoiceninja")
	ctx, today := context.Background(), time.Now()

	p, err := dunning.Open(ctx, d, who, "", today)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Cases) == 0 {
		t.Skip("the demo has no invoice due for a reminder today")
	}
	c := p.Cases[0]
	stage := func(n int) map[string]string {
		return map[string]string{dunning.FieldStage: string(rune('0' + n)), dunning.FieldSubject: "Erinnerung"}
	}
	steps := []workbench.Step{{Case: c.Key, Action: dunning.ActRemind, Fields: stage(c.Next)}, {Case: c.Key, Action: dunning.ActRemind, Fields: stage(c.Next + 1)}}
	plan, err := dunning.Review(ctx, d, who, "", steps, today)
	if err != nil || plan.Writes() != 1 || plan.Steps[1].Problem != dunning.ProblemStage || plan.Steps[0].Item.Subject != "Erinnerung" {
		t.Fatalf("review: %+v %v", plan, err)
	}
	if _, err := dunning.Apply(ctx, d, who, "", steps[:1], today, ""); !errors.Is(err, dunning.ErrDemo) {
		t.Fatalf("demo: %v", err)
	}
}
