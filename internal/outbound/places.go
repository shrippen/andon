package outbound

// Places: a Dawarich area and a Kimai mileage place for one location,
// kept in step by services/places.

import (
	"context"
	"net/http"
	"strconv"

	"andon/internal/drivers/services"
)

// Area is a Dawarich area: a circle with a name.
type Area struct {
	Name     string
	Lat, Lon float64
	Radius   float64 // metres
}

// DawarichCreateArea creates an area and returns its id. The fields go
// both flat and wrapped in "area", as Rails reads either.
func DawarichCreateArea(ctx context.Context, to Target, a Area) (int64, error) {
	api := services.DawarichApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
	fields := map[string]any{"name": a.Name, "latitude": a.Lat, "longitude": a.Lon, "radius": a.Radius}
	body := map[string]any{"area": fields}
	for k, v := range fields {
		body[k] = v
	}
	raw, err := api.Send(ctx, http.MethodPost, "areas", body)
	if err != nil {
		return 0, err
	}
	return idOf(raw), nil
}

// MileagePlace is a place of the Kimai mileage plugin; zero ids are
// left out, CustomerID -1 clears the customer.
type MileagePlace struct {
	Name            string
	Type            string // home, work, customer, other
	CustomerID      int64
	Lat, Lon        float64
	Radius          float64
	AreaID, PlaceID int64
}

// noCustomer clears a place's customer.
const noCustomer = -1

func (p MileagePlace) body() map[string]any {
	out := map[string]any{}
	if p.Name != "" {
		out["name"] = p.Name
	}
	if p.Type != "" {
		out["type"] = p.Type
	}
	switch {
	case p.CustomerID == noCustomer:
		out["customerId"] = nil
	case p.CustomerID > 0:
		out["customerId"] = p.CustomerID
	}
	if p.Lat != 0 || p.Lon != 0 {
		out["latitude"], out["longitude"] = p.Lat, p.Lon
	}
	if p.Radius > 0 {
		out["radius"] = int(p.Radius)
	}
	if p.AreaID > 0 {
		out["dawarichAreaId"] = p.AreaID
	}
	if p.PlaceID > 0 {
		out["dawarichPlaceId"] = p.PlaceID
	}
	return out
}

// ClearCustomer marks a write that removes the place's customer.
func (p MileagePlace) ClearCustomer() MileagePlace {
	p.CustomerID = noCustomer
	return p
}

// KimaiCreatePlace creates a mileage place and returns its id.
func KimaiCreatePlace(ctx context.Context, to Target, p MileagePlace) (int64, error) {
	api := services.KimaiApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
	raw, err := api.Send(ctx, http.MethodPost, "mileage/places", p.body())
	if err != nil {
		return 0, err
	}
	return idOf(raw), nil
}

// KimaiUpdatePlace changes the fields of p that are set.
func KimaiUpdatePlace(ctx context.Context, to Target, id int64, p MileagePlace) error {
	api := services.KimaiApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
	_, err := api.Send(ctx, http.MethodPatch, "mileage/places/"+strconv.FormatInt(id, 10), p.body())
	return err
}

// idOf reads "id" of a JSON object answer.
func idOf(raw any) int64 {
	m, _ := raw.(map[string]any)
	switch id := m["id"].(type) {
	case float64:
		return int64(id)
	case string:
		n, _ := strconv.ParseInt(id, 10, 64)
		return n
	}
	return 0
}
