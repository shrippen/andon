package widgets

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// The homelab cost popup lists the cost per Komodo stack.
func TestHomelabCostPerStack(t *testing.T) {
	now := time.Now()
	results := map[string]any{
		string(enums.ServiceSnipeIT): sources.DemoSnipe(now),
		string(enums.ServiceKomodo):  sources.DemoKomodo(now),
	}
	view := homelabCostView(CostConfig{}, results, ViewCtx{})
	stacks, _ := view["Stacks"].([]metrics.StackCost)
	if len(stacks) == 0 || stacks[0].Monthly <= 0 {
		t.Fatalf("stacks: %+v (bill %+v)", stacks, view["Bill"])
	}
	detail := homelabCostDetail(CostConfig{}, results, ViewCtx{})
	found := false
	body, _ := detail.Body.(*DetailBody)
	if body == nil {
		t.Fatal("no popup body")
	}
	for _, b := range body.Blocks {
		if b.Kind == BlockTable {
			found = true
		}
	}
	if !found {
		t.Error("no cost table in the popup")
	}
}
