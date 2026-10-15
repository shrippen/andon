package outbound

// Writes of the inventory bench: a device the network knows becomes a
// Snipe-IT asset.
//
//	POST   hardware        {name, model_id, status_id} → its id
//	PATCH  hardware/{id}   {warranty_months} (purchase bench)
//	DELETE hardware/{id}   (live tests clean up)

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"andon/internal/drivers/services"
)

// SnipeNew is an asset to add; Snipe-IT numbers its tag when it is set
// to (Settings → Asset tags), else Tag names it.
type SnipeNew struct {
	Name, Tag, Serial string
	Model, Status     int64
	Notes             string
	Bought            string // purchase date, "2026-10-01"; "" none
}

func snipe(to Target) services.SnipeApi {
	return services.SnipeApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
}

// SnipeAssetCreate adds an asset and returns its id.
func SnipeAssetCreate(ctx context.Context, to Target, a SnipeNew) (int64, error) {
	body := map[string]any{"name": a.Name, "model_id": a.Model, "status_id": a.Status}
	if a.Tag != "" {
		body["asset_tag"] = a.Tag
	}
	if a.Serial != "" {
		body["serial"] = a.Serial
	}
	if a.Notes != "" {
		body["notes"] = a.Notes
	}
	if a.Bought != "" {
		body["purchase_date"] = a.Bought
	}
	res, err := snipe(to).Send(ctx, http.MethodPost, "hardware", body)
	if err != nil {
		return 0, err
	}
	payload, _ := res.(map[string]any)["payload"].(map[string]any)
	id, _ := payload["id"].(float64)
	if id == 0 {
		return 0, errors.New("snipe-it: no asset id in the answer")
	}
	return int64(id), nil
}

// SnipeAssetDelete removes an asset (Snipe-IT keeps it as deleted).
func SnipeAssetDelete(ctx context.Context, to Target, id int64) error {
	_, err := snipe(to).Send(ctx, http.MethodDelete, "hardware/"+strconv.FormatInt(id, 10), nil)
	return err
}

// SnipeWarranty sets an asset's warranty in months; Snipe-IT counts it
// from the purchase date.
func SnipeWarranty(ctx context.Context, to Target, id int64, months int) error {
	_, err := snipe(to).Send(ctx, http.MethodPatch, "hardware/"+strconv.FormatInt(id, 10), map[string]any{"warranty_months": months})
	return err
}
