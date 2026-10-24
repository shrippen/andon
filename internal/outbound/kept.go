package outbound

// Writes to Kept: a thing for a device the network knows (inventory
// bench), a warranty for a purchase (purchases bench). Kept keeps
// warranties and the trash from REST tokens: warranties go through its
// MCP tool, and nothing is deleted.
//
//	GET    api/v1/locations/{id}/places  → the location's "Unplaced"
//	POST   api/v1/things                 {locationId, placeId, name, serial, notes} → its id
//	MCP    add_warranty                  {thing_id, kind, term_months}

import (
	"context"
	"errors"
	"net/http"

	"andon/internal/drivers/services"
)

const (
	keptThings       = "api/v1/things"
	keptLocations    = "api/v1/locations/"
	keptPlaces       = "/places"
	keptManufacturer = "manufacturer"
	keptWarrantyTool = "add_warranty"
)

// ErrKeptNoPlace means the location has no "Unplaced" place to put a new
// thing in.
var ErrKeptNoPlace = errors.New("kept: location without unplaced place")

// KeptNew is a thing to add to a location.
type KeptNew struct {
	Location     string // Kept's location id
	Name, Serial string
	Notes        string
}

func kept(to Target) services.KeptApi {
	return services.KeptApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
}

// KeptThingCreate adds a thing to the location's "Unplaced" and returns
// its id.
func KeptThingCreate(ctx context.Context, to Target, t KeptNew) (string, error) {
	api := kept(to)
	place, err := keptUnplaced(ctx, api, t.Location)
	if err != nil {
		return "", err
	}
	body := map[string]any{"locationId": t.Location, "placeId": place, "name": t.Name}
	if t.Serial != "" {
		body["serial"] = t.Serial
	}
	if t.Notes != "" {
		body["notes"] = t.Notes
	}
	raw, err := api.Send(ctx, http.MethodPost, keptThings, body, 0)
	if err != nil {
		return "", err
	}
	id, _ := raw.(map[string]any)["id"].(string)
	if id == "" {
		return "", errors.New("kept: no id in answer")
	}
	return id, nil
}

// keptUnplaced is the location's place for things without one.
func keptUnplaced(ctx context.Context, api services.KeptApi, location string) (string, error) {
	raw, err := api.Get(ctx, keptLocations+location+keptPlaces, nil)
	if err != nil {
		return "", err
	}
	places, _ := raw.(map[string]any)["places"].([]any)
	for _, p := range places {
		m, _ := p.(map[string]any)
		if unplaced, _ := m["isUnplaced"].(bool); unplaced {
			id, _ := m["id"].(string)
			return id, nil
		}
	}
	return "", ErrKeptNoPlace
}

// KeptWarranty records the manufacturer's warranty of months on a
// thing; Kept counts them from the purchase date.
func KeptWarranty(ctx context.Context, to Target, thing string, months int) error {
	_, err := kept(to).Tool(ctx, keptWarrantyTool, map[string]any{"thing_id": thing, "kind": keptManufacturer, "term_months": months})
	return err
}
