package outbound_test

import (
	"context"
	"testing"
	"time"

	"andon/internal/outbound"
	"andon/internal/sources"
	"andon/internal/testkit/live"
)

// kindAsset is a Snipe-IT test entry in the write log.
const kindAsset = "asset"

// An asset added under the first model and a deployable status, read
// back, given a warranty and deleted: the writes of the inventory and
// purchase benches.
func TestSnipeLive(t *testing.T) {
	to := live.Target(t, live.Snipe)
	ctx := context.Background()
	name := live.Name(t)

	src, _ := sources.Get("snipeit.data")
	out, err := src.Fetch(ctx, sources.Ctx{URL: to.URL, Secret: to.Token, VerifyTLS: to.VerifyTLS})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	data := out.(*sources.SnipeDataset)
	if len(data.Models) == 0 || len(data.Statuses) == 0 {
		t.Fatalf("models %d, statuses %d", len(data.Models), len(data.Statuses))
	}
	status := data.Statuses[0].ID
	for _, s := range data.Statuses {
		if s.Kind == "deployable" {
			status = s.ID
		}
	}

	bought := time.Now().AddDate(0, -1, 0).Format(time.DateOnly)
	id, err := outbound.SnipeAssetCreate(ctx, to, outbound.SnipeNew{Name: name, Tag: name, Model: data.Models[0].ID, Status: status, Notes: name, Bought: bought})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	live.Created(t, live.Snipe, kindAsset, id)
	t.Cleanup(func() {
		live.Change(t, live.Snipe, live.Delete, kindAsset, id)
		if err := outbound.SnipeAssetDelete(ctx, to, id); err != nil {
			t.Errorf("delete: %v", err)
		}
	})

	if a := snipeAsset(t, to, id); a.Name != name || a.PurchaseDate != bought {
		t.Fatalf("asset after create: %+v", a)
	}

	live.Change(t, live.Snipe, live.Update, kindAsset, id)
	if err := outbound.SnipeWarranty(ctx, to, id, 24); err != nil {
		t.Fatalf("warranty: %v", err)
	}
	if a := snipeAsset(t, to, id); a.WarrantyExpires == "" {
		t.Errorf("no warranty after setting it: %+v", a)
	}
}

// snipeAsset reads asset id fresh.
func snipeAsset(t *testing.T, to outbound.Target, id int64) sources.SnipeAsset {
	t.Helper()
	src, _ := sources.Get("snipeit.data")
	out, err := src.Fetch(context.Background(), sources.Ctx{URL: to.URL, Secret: to.Token, VerifyTLS: to.VerifyTLS})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, a := range out.(*sources.SnipeDataset).Assets {
		if a.ID == id {
			return a
		}
	}
	t.Fatalf("asset %d not found", id)
	return sources.SnipeAsset{}
}
