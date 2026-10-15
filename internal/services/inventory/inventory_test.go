package inventory_test

import (
	"context"
	"errors"
	"testing"

	"andon/internal/enums"
	"andon/internal/services/inventory"
	"andon/internal/services/workbench"
	"andon/internal/testkit"
)

// The demo network's unknown devices reach the review: an asset needs a
// model; an ignore is written to the space settings, an add refused by
// the demo.
func TestInventory(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	for _, s := range []enums.ServiceType{enums.ServiceSnipeIT, enums.ServiceFritzBox} {
		testkit.Conn(t, d, who, space, s, "demo://"+string(s))
	}
	ctx := context.Background()

	page, err := inventory.Open(ctx, d, who, "")
	if err != nil || len(page.Cases) < 2 || len(page.Models) == 0 || page.Status == 0 {
		t.Fatalf("open: %+v %v", page, err)
	}
	first, second := page.Cases[0].Key(), page.Cases[1].Key()

	add := workbench.Step{Case: first, Action: inventory.ActAdd, Fields: map[string]string{inventory.FieldModel: "sonstiges netzwerkgerät", inventory.FieldStatus: "1"}}
	steps := []workbench.Step{add, {Case: second, Action: inventory.ActAdd}, {Case: second, Action: inventory.ActIgnore}}
	plan, err := inventory.Review(ctx, d, who, "", steps)
	if err != nil || plan.Writes() != 2 || plan.Steps[1].Problem != inventory.ProblemChoice {
		t.Fatalf("review: %+v %v", plan, err)
	}
	if _, err := inventory.Apply(ctx, d, who, "", steps[:1], ""); !errors.Is(err, inventory.ErrDemo) {
		t.Fatalf("demo add: %v", err)
	}

	res, err := inventory.Apply(ctx, d, who, "", steps[2:], "")
	if err != nil || res.Done != 1 {
		t.Fatalf("ignore: %+v %v", res, err)
	}
	if page, _ = inventory.Open(ctx, d, who, ""); containsKey(page, second) || !containsKey(page, first) {
		t.Errorf("ignored device still listed: %+v", page.Cases)
	}
}

func containsKey(p inventory.Page, key string) bool {
	for _, c := range p.Cases {
		if c.Key() == key {
			return true
		}
	}
	return false
}
