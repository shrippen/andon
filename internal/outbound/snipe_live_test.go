package outbound_test

import (
	"context"
	"testing"

	"andon/internal/outbound"
	"andon/internal/sources"
	"andon/internal/testkit/live"
)

// kindAsset is a Snipe-IT test entry in the write log.
const kindAsset = "asset"

// An asset added under the first model and a deployable status, read
// back, and deleted: the write of the inventory bench.
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

	id, err := outbound.SnipeAssetCreate(ctx, to, outbound.SnipeNew{Name: name, Tag: name, Model: data.Models[0].ID, Status: status, Notes: name})
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

	out, err = src.Fetch(ctx, sources.Ctx{URL: to.URL, Secret: to.Token, VerifyTLS: to.VerifyTLS})
	if err != nil {
		t.Fatalf("read again: %v", err)
	}
	for _, a := range out.(*sources.SnipeDataset).Assets {
		if a.ID == id && a.Name == name {
			return
		}
	}
	t.Errorf("asset %d %q not found after create", id, name)
}
