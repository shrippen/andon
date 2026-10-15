package subs_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/services/subs"
	"andon/internal/services/workbench"
	"andon/internal/testkit"
)

// The demo's cases reach the review: a new price with its amount, a
// twin without fields; a missing amount and a settled case are refused,
// and the demo takes no writes.
func TestSubs(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	for _, s := range []enums.ServiceType{enums.ServiceSure, enums.ServiceWallos, enums.ServiceInvoiceNinja} {
		testkit.Conn(t, d, who, space, s, "demo://"+string(s))
	}
	ctx, now := context.Background(), time.Now()

	page, err := subs.Open(ctx, d, who, "", now)
	if err != nil || !page.Wallos || !page.Ninja {
		t.Fatalf("open: %+v %v", page, err)
	}
	actions := map[string]metrics.SubAction{}
	for _, c := range page.Cases {
		actions[c.Key] = c.Action
	}
	if actions["np-demo-r1"] != metrics.SubNinjaPrice || actions["wm-5"] != metrics.SubWallosMerge {
		t.Fatalf("cases: %v", actions)
	}

	steps := []workbench.Step{
		{Case: "np-demo-r1", Action: string(metrics.SubNinjaPrice), Fields: map[string]string{subs.FieldAmount: "44,90"}},
		{Case: "wm-5", Action: string(metrics.SubWallosMerge)},
		{Case: "wp-1", Action: string(metrics.SubWallosPrice), Fields: map[string]string{subs.FieldAmount: ""}},
		{Case: "np-demo-r1", Action: string(metrics.SubNinjaEnd)},
	}
	plan, err := subs.Review(ctx, d, who, "", now, steps)
	if err != nil || plan.Writes() != 2 {
		t.Fatalf("review: %+v %v", plan, err)
	}
	if plan.Steps[0].Item.Amount != 44.9 || plan.Steps[0].Item.Old != 41.65 {
		t.Errorf("price: %+v", plan.Steps[0].Item)
	}
	if plan.Steps[2].Problem != subs.ProblemAmount || plan.Steps[3].Problem != subs.ProblemGone {
		t.Errorf("problems: %q %q", plan.Steps[2].Problem, plan.Steps[3].Problem)
	}

	if _, err := subs.Apply(ctx, d, who, "", now, steps[:2], ""); !errors.Is(err, subs.ErrDemo) {
		t.Fatalf("demo write: %v", err)
	}
}

// Without Sure there is nothing to compare against.
func TestSubsNeedSure(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	testkit.Conn(t, d, who, space, enums.ServiceWallos, "demo://wallos")
	if _, err := subs.Open(context.Background(), d, who, "", time.Now()); !errors.Is(err, subs.ErrNoSure) {
		t.Fatalf("open: %v", err)
	}
}
