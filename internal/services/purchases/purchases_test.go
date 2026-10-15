package purchases_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/services/purchases"
	"andon/internal/services/workbench"
	"andon/internal/testkit"
)

// A demo asset without warranty reaches the review; nonsense months are
// refused and the demo takes no writes.
func TestPurchases(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	testkit.Conn(t, d, who, space, enums.ServiceSnipeIT, "demo://snipeit")
	ctx, now := context.Background(), time.Now()

	page, err := purchases.Open(ctx, d, who, "", now)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	key := ""
	for _, c := range page.Cases {
		if c.Has(metrics.GapWarranty) {
			key = c.Key
		}
	}
	if key == "" {
		t.Fatalf("no demo asset without warranty: %+v", page.Cases)
	}
	steps := []workbench.Step{
		{Case: key, Action: purchases.ActWarranty, Fields: map[string]string{purchases.FieldMonths: "24"}},
		{Case: key, Action: purchases.ActWarranty, Fields: map[string]string{purchases.FieldMonths: "999"}},
	}
	plan, err := purchases.Review(ctx, d, who, "", now, steps)
	if err != nil || plan.Writes() != 1 || plan.Steps[1].Problem != purchases.ProblemMonths {
		t.Fatalf("review: %+v %v", plan, err)
	}
	if _, err := purchases.Apply(ctx, d, who, "", now, steps[:1], ""); !errors.Is(err, purchases.ErrDemo) {
		t.Fatalf("demo write: %v", err)
	}
}
