package outbound_test

import (
	"context"
	"testing"

	"andon/internal/outbound"
	"andon/internal/sources"
	"andon/internal/testkit/live"
)

// kindThing is a Kept test entry in the write log.
const kindThing = "thing"

// A thing added to the token's first location, then a warranty on it:
// the writes of the inventory and purchase benches. Kept keeps the trash
// from tokens, so the andon-test thing stays in the throwaway container.
func TestKeptLive(t *testing.T) {
	to := live.Target(t, live.Kept)
	ctx := context.Background()
	name := live.Name(t)

	data := keptRead(t, to)
	if len(data.Locations) == 0 || !data.Writable {
		t.Fatalf("locations %+v, writable %v (read+write token and the MCP module needed)", data.Locations, data.Writable)
	}
	id, err := outbound.KeptThingCreate(ctx, to, outbound.KeptNew{Location: data.Locations[0].ID, Name: name, Serial: name, Notes: name})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	live.Created(t, live.Kept, kindThing, id)

	live.Change(t, live.Kept, live.Update, kindThing, id)
	if err := outbound.KeptWarranty(ctx, to, id, 24); err != nil {
		t.Fatalf("warranty: %v", err)
	}
	for _, th := range keptRead(t, to).Things {
		if th.ID == id {
			if th.Name != name || th.WarrantyEnds == "" {
				t.Errorf("thing after writes: %+v", th)
			}
			return
		}
	}
	t.Errorf("thing %s not read back", id)
}

func keptRead(t *testing.T, to outbound.Target) *sources.KeptDataset {
	t.Helper()
	out, err := sources.KeptData.Fetch(context.Background(), sources.Ctx{URL: to.URL, Secret: to.Token, VerifyTLS: to.VerifyTLS})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return out.(*sources.KeptDataset)
}
