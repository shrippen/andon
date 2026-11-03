package sites_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/services/learn"
	"andon/internal/services/sites"
	"andon/internal/services/svcdata"
	"andon/internal/testkit"
)

// rideStart is when the fake track's drive home → Acme begins.
var rideStart = time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)

// oneRide are Dawarich answers with one track: a 30-minute drive of
// 25 km from home to Acme.
func oneRide() map[string]string {
	end := rideStart.Add(30 * time.Minute)
	return map[string]string{
		"/api/v1/tracks": `{"features": [{"properties": {"id": 1, "revision": 1, "end_at": "` + end.Format(time.RFC3339) + `"}}]}`,
		"/api/v1/tracks/1": `{"features": [{"properties": {"id": 1, "start_at": "` + rideStart.Format(time.RFC3339) + `", "end_at": "` + end.Format(time.RFC3339) + `",
			"segments": [{"start_time": ` + strconv.FormatInt(rideStart.Unix(), 10) + `, "end_time": ` + strconv.FormatInt(end.Unix(), 10) + `,
			"mode": "driving", "distance": 25000, "coordinates": [[13.4, 52.52], [13.06, 52.4]]}]}}]}`,
	}
}

// ridesOption is the connection's classes set by hand.
func ridesOption(t *testing.T, d *sql.DB, conn int64) map[string]any {
	t.Helper()
	c, err := connections.ByID(d, conn)
	if err != nil {
		t.Fatal(err)
	}
	all, _ := c.Options[metrics.OptionRides].(map[string]any)
	return all
}

// Without the plugin a class set by hand lives in the connection's
// option until it is handed back to the rules.
func TestSetClassWithoutPlugin(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	rec := &recorder{}
	geo := fakeDawarich(rec, oneRide())
	defer geo.Close()
	conn := testkit.Conn(t, d, who, space, enums.ServiceDawarich, geo.URL)
	ctx := context.Background()
	key := strconv.FormatInt(rideStart.Unix(), 10)

	if err := sites.SetClass(ctx, d, who, conn, rideStart.Unix(), metrics.ClassBusiness); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := ridesOption(t, d, conn); got[key] != "business" {
		t.Fatalf("option: %+v", got)
	}

	if err := sites.SetClass(ctx, d, who, conn, rideStart.Unix(), metrics.ClassAuto); err != nil {
		t.Fatalf("auto: %v", err)
	}
	if got := ridesOption(t, d, conn); len(got) != 0 {
		t.Fatalf("handed back: %+v", got)
	}

	if err := sites.SetClass(ctx, d, who, conn, rideStart.Unix()+1, metrics.ClassPrivate); !errors.Is(err, sites.ErrUnknownRide) {
		t.Fatalf("no ride then: %v", err)
	}
	if err := sites.SetClass(ctx, d, who, conn, rideStart.Unix(), "holiday"); !errors.Is(err, sites.ErrBadClass) {
		t.Fatalf("no class: %v", err)
	}
}

// With the plugin a car ride becomes a mileage trip there; Andon keeps
// no copy.
func TestSetClassWithPlugin(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	rec := &recorder{}
	geo, kimai := fakeDawarich(rec, oneRide()), fakeKimai(rec, pluginWrites)
	defer geo.Close()
	defer kimai.Close()
	conn := testkit.Conn(t, d, who, space, enums.ServiceDawarich, geo.URL)
	testkit.Conn(t, d, who, space, enums.ServiceKimai, kimai.URL)

	if err := sites.SetClass(context.Background(), d, who, conn, rideStart.Unix(), metrics.ClassPrivate); err != nil {
		t.Fatalf("set: %v", err)
	}
	writes := rec.list()
	if len(writes) != 1 || writes[0].Call != http.MethodPost+" /api/mileage/trips" {
		t.Fatalf("writes: %+v", writes)
	}
	body := writes[0].Body
	if body["purpose"] != "private" || body["distanceKm"] != 25.0 || body["destination"] != "Acme" || body["vehicle"] != nil {
		t.Fatalf("trip: %+v", body)
	}
	if got := ridesOption(t, d, conn); len(got) != 0 {
		t.Fatalf("no copy in Andon: %+v", got)
	}
}

// A read-only plugin is left alone: the class stays in the option.
func TestSetClassReadOnlyPlugin(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	rec := &recorder{}
	geo, kimai := fakeDawarich(rec, oneRide()), fakeKimai(rec, pluginReads)
	defer geo.Close()
	defer kimai.Close()
	conn := testkit.Conn(t, d, who, space, enums.ServiceDawarich, geo.URL)
	testkit.Conn(t, d, who, space, enums.ServiceKimai, kimai.URL)

	if err := sites.SetClass(context.Background(), d, who, conn, rideStart.Unix(), metrics.ClassPrivate); err != nil {
		t.Fatalf("set: %v", err)
	}
	if writes := rec.list(); len(writes) != 0 {
		t.Fatalf("nothing written to Kimai: %+v", writes)
	}
	if got := ridesOption(t, d, conn); got[strconv.FormatInt(rideStart.Unix(), 10)] != "private" {
		t.Fatalf("option: %+v", got)
	}
}

// The old plugin still takes trips (editOwn is enough).
func TestSetClassOldPlugin(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	rec := &recorder{}
	geo, kimai := fakeDawarich(rec, oneRide()), fakeKimai(rec, pluginOld)
	defer geo.Close()
	defer kimai.Close()
	conn := testkit.Conn(t, d, who, space, enums.ServiceDawarich, geo.URL)
	testkit.Conn(t, d, who, space, enums.ServiceKimai, kimai.URL)

	if err := sites.SetClass(context.Background(), d, who, conn, rideStart.Unix(), metrics.ClassBusiness); err != nil {
		t.Fatalf("set: %v", err)
	}
	if writes := rec.list(); len(writes) != 1 || writes[0].Call != http.MethodPost+" /api/mileage/trips" {
		t.Fatalf("writes: %+v", writes)
	}
}

// A bicycle ride's class stays in Andon even with the plugin: it pays
// only car and motorbike km.
func TestSetClassBicycleStaysInAndon(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	rec := &recorder{}
	answers := oneRide()
	answers["/api/v1/tracks/1"] = strings.Replace(answers["/api/v1/tracks/1"], `"mode": "driving"`, `"mode": "cycling"`, 1)
	geo, kimai := fakeDawarich(rec, answers), fakeKimai(rec, pluginWrites)
	defer geo.Close()
	defer kimai.Close()
	conn := testkit.Conn(t, d, who, space, enums.ServiceDawarich, geo.URL)
	testkit.Conn(t, d, who, space, enums.ServiceKimai, kimai.URL)

	if err := sites.SetClass(context.Background(), d, who, conn, rideStart.Unix(), metrics.ClassBusiness); err != nil {
		t.Fatalf("set: %v", err)
	}
	if writes := rec.list(); len(writes) != 0 {
		t.Fatalf("written to the plugin: %+v", writes)
	}
	if got := ridesOption(t, d, conn); got[strconv.FormatInt(rideStart.Unix(), 10)] != "business" {
		t.Fatalf("option: %+v", got)
	}
}

// rideExamples is how many rides the connection's ride model holds.
func rideExamples(t *testing.T, d *sql.DB, who *access.Principal, conn int64) int {
	t.Helper()
	c, err := connections.ByID(d, conn)
	if err != nil {
		t.Fatal(err)
	}
	return sites.Learned(d, c, who).Model.Stats().Examples
}

// A class set by hand teaches the ride model, also when the plugin keeps
// it; handed back to the rules, the ride is forgotten.
func TestSetClassTeaches(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	rec := &recorder{}
	geo, kimai := fakeDawarich(rec, oneRide()), fakeKimai(rec, pluginWrites)
	defer geo.Close()
	defer kimai.Close()
	conn := testkit.Conn(t, d, who, space, enums.ServiceDawarich, geo.URL)
	testkit.Conn(t, d, who, space, enums.ServiceKimai, kimai.URL)
	ctx := context.Background()

	if err := sites.SetClass(ctx, d, who, conn, rideStart.Unix(), metrics.ClassBusiness); err != nil {
		t.Fatalf("set: %v", err)
	}
	if n := rideExamples(t, d, who, conn); n != 1 {
		t.Fatalf("plugin trip learned: %d", n)
	}
	if err := sites.SetClass(ctx, d, who, conn, rideStart.Unix(), metrics.ClassAuto); err != nil {
		t.Fatalf("auto: %v", err)
	}
	if n := rideExamples(t, d, who, conn); n != 0 {
		t.Fatalf("handed back: %d", n)
	}
}

// The job learns the classes set by hand before the model existed.
func TestTrainLearnsStoredClasses(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	rec := &recorder{}
	geo := fakeDawarich(rec, oneRide())
	defer geo.Close()
	conn := testkit.Conn(t, d, who, space, enums.ServiceDawarich, geo.URL)
	key := strconv.FormatInt(rideStart.Unix(), 10)
	if err := connections.SetOptions(d, who, conn, map[string]any{metrics.OptionRides: map[string]any{key: "private"}}); err != nil {
		t.Fatal(err)
	}
	svcdata.Forget(conn)

	if err := learn.Train(context.Background(), d, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n := rideExamples(t, d, who, conn); n != 1 {
		t.Fatalf("stored class learned: %d", n)
	}
}

// A class the plugin keeps (a mileage trip) is learned by the job too:
// after a reset it comes back from Kimai.
func TestTrainLearnsPluginClasses(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleAdmin)
	rec := &recorder{}
	end := rideStart.Add(30 * time.Minute)
	trip := `[{"id": 7, "departure": "` + rideStart.Format(time.RFC3339) + `", "arrival": "` + end.Format(time.RFC3339) + `", "purpose": "private"}]`
	geo, kimai := fakeDawarich(rec, oneRide()), fakeKimai(rec, pluginWrites, trip)
	defer geo.Close()
	defer kimai.Close()
	conn := testkit.Conn(t, d, who, space, enums.ServiceDawarich, geo.URL)
	testkit.Conn(t, d, who, space, enums.ServiceKimai, kimai.URL)
	ctx := context.Background()

	if err := learn.Train(ctx, d, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n := rideExamples(t, d, who, conn); n != 1 {
		t.Fatalf("plugin class learned: %d", n)
	}
	if err := learn.Reset(d, who, sites.PurposeRides, conn, model.NoHolder, ""); err != nil {
		t.Fatal(err)
	}
	if err := learn.Train(ctx, d, time.Now()); err != nil {
		t.Fatal(err)
	}
	if n := rideExamples(t, d, who, conn); n != 1 {
		t.Fatalf("learned again after reset: %d", n)
	}
}
