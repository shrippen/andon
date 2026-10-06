package outbound_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/outbound"
	"andon/internal/testkit/live"
)

// Kinds of further Kimai test entries in the write log.
const (
	kindCustomer = "customer"
	kindTrip     = "trip"
)

// A sheet marked exported and back, then deleted. Kimai's export call
// toggles the flag; an exported sheet cannot be deleted.
func TestKimaiExportLive(t *testing.T) {
	kimai := live.Target(t, live.Kimai)
	api := kimaiAPI(kimai)
	ctx := context.Background()
	name := live.Name(t)
	project, activity := kimaiPair(t, api)

	begin := time.Now().AddDate(0, 0, -1).Truncate(time.Hour)
	sheet := outbound.KimaiSheet{
		Project: project, Activity: activity, Description: name, Billable: enums.BillableNo,
		Begin: begin.Format(kimaiTime), End: begin.Add(15 * time.Minute).Format(kimaiTime),
	}
	if err := outbound.KimaiCreate(ctx, kimai, sheet); err != nil {
		t.Fatalf("create: %v", err)
	}
	id := kimaiSheet(t, api, "timesheets", url.Values{"term": {name}, "full": {"true"}}, name)
	live.Created(t, live.Kimai, kindSheet, id)
	t.Cleanup(func() { kimaiDelete(t, kimai, id) })

	live.Change(t, live.Kimai, live.Update, kindSheet, id)
	if err := outbound.KimaiMarkExported(ctx, kimai, id); err != nil {
		t.Fatalf("mark exported: %v", err)
	}
	if e := kimaiRead(t, api, id)["exported"]; e != true {
		t.Errorf("exported = %v, want true", e)
	}

	// Back, so the cleanup can delete it.
	live.Change(t, live.Kimai, live.Update, kindSheet, id)
	if _, err := api.Send(ctx, http.MethodPatch, "timesheets/"+strconv.FormatInt(id, 10)+"/export", nil); err != nil {
		t.Errorf("unexport: %v", err)
	}
}

// A customer of its own renamed, then deleted.
func TestKimaiCustomerLive(t *testing.T) {
	kimai := live.Target(t, live.Kimai)
	api := kimaiAPI(kimai)
	ctx := context.Background()
	name := live.Name(t)

	raw, err := api.Send(ctx, http.MethodPost, "customers", map[string]any{
		"name": name, "country": "DE", "currency": "EUR", "timezone": "Europe/Berlin", "visible": true,
	})
	if err != nil {
		t.Fatalf("create customer: %v", err)
	}
	id := num(asObject(raw)["id"])
	if id == 0 {
		t.Fatal("create customer: no id in answer")
	}
	live.Created(t, live.Kimai, kindCustomer, id)
	t.Cleanup(func() { kimaiRemove(t, api, kindCustomer, "customers/", id) })

	renamed := name + " renamed"
	live.Change(t, live.Kimai, live.Update, kindCustomer, id)
	if err := outbound.KimaiRenameCustomer(ctx, kimai, id, renamed); err != nil {
		t.Fatalf("rename: %v", err)
	}
	got, err := api.Get(ctx, "customers/"+strconv.FormatInt(id, 10), nil)
	if err != nil {
		t.Fatalf("read customer: %v", err)
	}
	if n := asObject(got)["name"]; n != renamed {
		t.Errorf("name = %v, want %q", n, renamed)
	}
}

// A trip of the mileage plugin booked yesterday, its purpose changed,
// then deleted.
func TestKimaiTripLive(t *testing.T) {
	kimai := live.Target(t, live.Kimai)
	api := kimaiAPI(kimai)
	ctx := context.Background()
	name := live.Name(t)

	departure := time.Now().AddDate(0, 0, -1).Truncate(time.Hour)
	trip := outbound.MileageTrip{
		Departure: departure, Arrival: departure.Add(10 * time.Minute),
		Purpose: "private", KM: 1, Start: name, Destination: name,
	}
	id, err := outbound.KimaiCreateTrip(ctx, kimai, trip)
	if err != nil {
		t.Fatalf("create trip: %v", err)
	}
	if id == 0 {
		t.Fatal("create trip: no id in answer")
	}
	live.Created(t, live.Kimai, kindTrip, id)
	t.Cleanup(func() { kimaiRemove(t, api, kindTrip, "mileage/trips/", id) })

	live.Change(t, live.Kimai, live.Update, kindTrip, id)
	if err := outbound.KimaiTripPurpose(ctx, kimai, id, "business"); err != nil {
		t.Fatalf("purpose: %v", err)
	}
	got, err := api.Get(ctx, "mileage/trips/"+strconv.FormatInt(id, 10), nil)
	if err != nil {
		t.Fatalf("read trip: %v", err)
	}
	if p := asObject(got)["purpose"]; p != "business" {
		t.Errorf("purpose = %v, want business", p)
	}
}

// asObject reads a JSON object answer.
func asObject(raw any) map[string]any {
	m, _ := raw.(map[string]any)
	return m
}
