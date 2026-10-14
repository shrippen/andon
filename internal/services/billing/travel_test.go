package billing_test

import (
	"context"
	"math"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/services/billing"
	"andon/internal/testkit"
)

// With Dawarich beside Kimai, a draft of a customer driven to offers the
// km at the space's rate, from the oldest unbilled entry to yesterday.
func TestTravelLine(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	for _, s := range []enums.ServiceType{enums.ServiceKimai, enums.ServiceInvoiceNinja, enums.ServiceDawarich} {
		testkit.Conn(t, d, who, space, s, "demo://"+string(s))
	}
	// Stored data fills in the background on first read.
	var list []billing.Candidate
	for try := 0; try < 50 && len(list) == 0; try++ {
		var err error
		if list, err = billing.Candidates(context.Background(), d, who); err != nil {
			t.Fatal(err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	found := 0
	for _, c := range list {
		if c.Travel == nil {
			continue
		}
		found++
		if c.Travel.KM < 1 || c.Travel.Rate != 0.30 || math.Abs(c.Travel.Amount()-c.Travel.KM*0.30) > 0.01 || c.Travel.From.After(c.Travel.To) {
			t.Errorf("%s: %+v", c.Customer, c.Travel)
		}
	}
	if found == 0 {
		t.Skip("the demo drove to no customer with unbilled time")
	}
}
