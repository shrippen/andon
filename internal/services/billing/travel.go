package billing

// Km to bill a customer, on top of the hours: business rides by own car
// or motorbike to that customer (metrics.TravelOf), since the last bill
// that carried them, at the space's km rate.
//
//	Dawarich (the Kimai's partner) ─► rides ─► the customer's, from the day
//	after the last billed one (or the oldest unbilled entry) to yesterday
//
// The draft offers the line; ticked, it goes into the invoice and the
// day is kept, so no ride is billed twice.

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"strconv"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/services/access"
	"andon/internal/services/svcdata"
	"andon/internal/services/verbund"
	"andon/internal/services/workbench"
	"andon/internal/sources"
)

// TravelMode says whether a draft carries its km.
type TravelMode bool

const (
	NoTravel   TravelMode = false
	WithTravel TravelMode = true
)

// travelKey keeps, per Kimai and customer, the last day billed:
// {"3:12": "2026-09-30"}.
const travelKey = "billing_travel"

// TravelLine is the km to bill.
type TravelLine struct {
	From, To time.Time
	Rides    int
	KM       float64
	Rate     float64
}

// Amount is the line's total.
func (t TravelLine) Amount() float64 { return math.Round(t.KM*t.Rate*100) / 100 }

func travelSlot(kimaiID, customerID int64) string {
	return strconv.FormatInt(kimaiID, 10) + ":" + strconv.FormatInt(customerID, 10)
}

// travelOf is the customer's line, nil if no km are due.
func travelOf(ctx context.Context, d *sql.DB, who *access.Principal, p pair, kimai *sources.KimaiDataset, settings map[string]any, draft metrics.Draft, today time.Time) *TravelLine {
	geoConn, _, err := verbund.Partner(d, who, verbund.Asker{SpaceID: p.kimai.SpaceID, ConnID: p.kimai.ID}, enums.ServiceDawarich)
	if err != nil || geoConn == nil {
		return nil
	}
	key, holder := sources.DataKey(enums.ServiceDawarich), model.UserHolder(who.UserID)
	res, err := svcdata.Get(ctx, d, key, nil, geoConn, holder, svcdata.Stored)
	if err == nil && res.Pending {
		res, err = svcdata.Get(ctx, d, key, nil, geoConn, holder, svcdata.Cached)
	}
	geo, _ := res.Data.(*sources.DawarichDataset)
	if err != nil || geo == nil {
		return nil
	}
	billed, _ := workbench.LoadState[map[string]string](d, who, travelKey)
	from := draft.Oldest
	if last, ok := metrics.ParseDay(billed[travelSlot(p.kimai.ID, draft.CustomerID)]); ok {
		from = last.AddDate(0, 0, 1)
	}
	to := metrics.Today(today).AddDate(0, 0, -1)
	if from.IsZero() || from.After(to) {
		return nil
	}
	set := metrics.TravelSettingsOf(settings)
	travel := metrics.TravelOf(geo, kimai, geoConn.Options, set, today)
	var rides []metrics.ClassedRide
	for _, r := range travel.Between(from, to) {
		if r.CustomerID == draft.CustomerID {
			rides = append(rides, r)
		}
	}
	sum := metrics.ByCustomer(rides)[draft.CustomerID]
	if sum.PayKM < 1 {
		return nil
	}
	return &TravelLine{From: from, To: to, Rides: sum.Rides, KM: math.Round(sum.PayKM), Rate: set.KMRate}
}

// travelNote describes the line on the invoice.
func travelNote(t TravelLine) string {
	return fmt.Sprintf("%d × %s–%s, %.0f km", t.Rides, t.From.Format("02.01.2006"), t.To.Format("02.01.2006"), t.KM)
}

// keepTravel marks the line's days as billed.
func keepTravel(d *sql.DB, who *access.Principal, kimaiID, customerID int64, t TravelLine) error {
	return workbench.SaveState(d, who, travelKey, func(s *map[string]string) {
		if *s == nil {
			*s = map[string]string{}
		}
		(*s)[travelSlot(kimaiID, customerID)] = t.To.Format(time.DateOnly)
	})
}
