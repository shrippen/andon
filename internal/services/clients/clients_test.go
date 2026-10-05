package clients_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/clients"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
	"andon/internal/testkit"
)

// demoURL makes a connection serve the generated demo dataset.
const demoURL = "demo://studio"

// Customer cards come from the stored Kimai and Invoice Ninja run; a
// stranger sees none of them.
func TestListAndOneShowSpaceCustomers(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "owner@x.de", enums.RoleUser)
	stranger, _ := testkit.User(t, d, "other@x.de", enums.RoleUser)
	ctx := context.Background()

	// Seed the stored results the background run would leave behind.
	for _, service := range []enums.ServiceType{enums.ServiceKimai, enums.ServiceInvoiceNinja} {
		id := testkit.Conn(t, d, who, space, service, demoURL)
		conn, err := content.Connection(d, id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svcdata.Get(ctx, d, sources.DataKey(service), nil, conn, model.UserHolder(who.UserID), svcdata.Force); err != nil {
			t.Fatalf("seed %s: %v", service, err)
		}
	}

	cards, err := clients.List(ctx, d, who)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if want := len(sources.DemoKimai(time.Now()).Customers); len(cards) != want {
		t.Fatalf("cards: got %d, want %d", len(cards), want)
	}
	for i, c := range cards {
		if c.SpaceID != space || c.Currency == "" {
			t.Fatalf("card %d: %+v", i, c)
		}
		if i > 0 && c.HoursYear > cards[i-1].HoursYear {
			t.Fatalf("not sorted by hours: %v after %v", c.HoursYear, cards[i-1].HoursYear)
		}
	}

	first := cards[0]
	detail, err := clients.One(ctx, d, who, space, first.CustomerID)
	if err != nil || detail.Name != first.Name {
		t.Fatalf("one: %+v, %v", detail.Card, err)
	}
	if _, err := clients.One(ctx, d, who, space, -1); !errors.Is(err, clients.ErrNotFound) {
		t.Fatalf("unknown customer: %v", err)
	}

	if other, _ := clients.List(ctx, d, stranger); len(other) != 0 {
		t.Fatalf("stranger sees %d cards", len(other))
	}
	if _, err := clients.One(ctx, d, stranger, space, first.CustomerID); !errors.Is(err, clients.ErrNotFound) {
		t.Fatalf("stranger reads customer: %v", err)
	}
}
