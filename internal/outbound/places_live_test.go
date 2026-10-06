package outbound_test

import (
	"context"
	"net/http"
	"strconv"
	"testing"

	"andon/internal/drivers/services"
	"andon/internal/outbound"
	"andon/internal/testkit/live"
)

// Kinds of test entries in the write log.
const (
	kindArea  = "area"
	kindPlace = "place"
)

// A place created in Dawarich and in the Kimai plugin, then changed: the
// calls of services/places against real instances.
func TestPlacesLive(t *testing.T) {
	dawarich := live.Target(t, live.Dawarich)
	kimai := live.Target(t, live.Kimai)
	ctx := context.Background()
	name := live.Name(t)

	area := outbound.Area{Name: name, Lat: 52.52, Lon: 13.405, Radius: 50}
	areaID, err := outbound.DawarichCreateArea(ctx, dawarich, area)
	if err != nil {
		t.Fatalf("create area: %v", err)
	}
	if areaID == 0 {
		t.Fatal("create area: no id in answer")
	}
	live.Created(t, live.Dawarich, kindArea, areaID)
	t.Cleanup(func() { dawarichDelete(t, dawarich, areaID) })

	place := outbound.MileagePlace{Name: name, Type: "other", Lat: area.Lat, Lon: area.Lon, Radius: area.Radius, AreaID: areaID}
	placeID, err := outbound.KimaiCreatePlace(ctx, kimai, place)
	if err != nil {
		t.Fatalf("create place: %v", err)
	}
	if placeID == 0 {
		t.Fatal("create place: no id in answer")
	}
	live.Created(t, live.Kimai, kindPlace, placeID)

	live.Change(t, live.Kimai, live.Update, kindPlace, placeID)
	if err := outbound.KimaiUpdatePlace(ctx, kimai, placeID, outbound.MileagePlace{Radius: 80}); err != nil {
		t.Fatalf("update place: %v", err)
	}
}

// dawarichDelete removes the test's area; Andon itself never deletes
// areas.
func dawarichDelete(t *testing.T, to outbound.Target, id int64) {
	live.Change(t, live.Dawarich, live.Delete, kindArea, id)
	api := services.DawarichApi{URL: to.URL, Token: to.Token, Verify: to.VerifyTLS}
	if _, err := api.Send(context.Background(), http.MethodDelete, "areas/"+strconv.FormatInt(id, 10), nil); err != nil {
		t.Errorf("delete area %d: %v", id, err)
	}
}
