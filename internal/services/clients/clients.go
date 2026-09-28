// Package clients is the customer page: each Kimai customer with its
// Invoice Ninja client, one card with hours, unbilled work, invoices,
// revenue, payment habit and the hints that name it.
//
//	space ─► Kimai + Invoice Ninja (stored run data) ─► metrics.ClientCards
package clients

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/hints"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
)

// ErrNotFound means no such customer in a space the caller sees.
var ErrNotFound = errors.New("clients: not found")

// Card is one customer in one space.
type Card struct {
	SpaceID   int64
	SpaceName string
	Currency  string
	metrics.ClientCard
}

// Detail is a card with the hints that name the customer.
type Detail struct {
	Card
	Hints []hints.View
}

// space is a visible space with its Kimai and (optional) Ninja connection.
type space struct {
	ref          access.SpaceRef
	settings     map[string]any
	kimai, ninja *model.Connection
}

// List returns every customer of the caller's spaces, most hours first.
func List(ctx context.Context, d *sql.DB, who *access.Principal) ([]Card, error) {
	found, err := spaces(d, who)
	if err != nil {
		return nil, err
	}
	var out []Card
	for _, sp := range found {
		cards, err := cardsOf(ctx, d, who, sp)
		if err != nil {
			return nil, err
		}
		out = append(out, cards...)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].HoursYear > out[b].HoursYear })
	return out, nil
}

// One returns one customer with its hints.
func One(ctx context.Context, d *sql.DB, who *access.Principal, spaceID, customerID int64) (Detail, error) {
	found, err := spaces(d, who)
	if err != nil {
		return Detail{}, err
	}
	for _, sp := range found {
		if sp.ref.ID != spaceID {
			continue
		}
		cards, err := cardsOf(ctx, d, who, sp)
		if err != nil {
			return Detail{}, err
		}
		for _, c := range cards {
			if c.CustomerID == customerID {
				named, err := hintsNaming(d, who, c.Name)
				return Detail{Card: c, Hints: named}, err
			}
		}
	}
	return Detail{}, ErrNotFound
}

func spaces(d *sql.DB, who *access.Principal) ([]space, error) {
	var out []space
	err := db.WithRead(d, func(tx *sql.Tx) error {
		for id, ref := range who.Spaces {
			if access.SpaceRight(who, &ref) < enums.RightView {
				continue
			}
			conns, err := content.Connections(tx, []int64{id})
			if err != nil {
				return err
			}
			sp := space{ref: ref}
			if row, err := content.Space(tx, id); err == nil && row != nil {
				sp.settings = row.Settings
			}
			for _, c := range conns {
				switch enums.ServiceType(c.Service) {
				case enums.ServiceKimai:
					sp.kimai = c
				case enums.ServiceInvoiceNinja:
					sp.ninja = c
				}
			}
			if sp.kimai != nil {
				out = append(out, sp)
			}
		}
		return nil
	})
	return out, err
}

// cardsOf builds the space's cards from the last background run.
func cardsOf(ctx context.Context, d *sql.DB, who *access.Principal, sp space) ([]Card, error) {
	uid := who.UserID
	k, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceKimai), nil, sp.kimai, &uid, svcdata.Stored)
	if errors.Is(err, svcdata.ErrMissingCredential) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	kimai, ok := k.Data.(*sources.KimaiDataset)
	if !ok {
		return nil, nil
	}
	var ninja *sources.NinjaDataset
	if sp.ninja != nil {
		if n, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceInvoiceNinja), nil, sp.ninja, &uid, svcdata.Stored); err == nil {
			ninja, _ = n.Data.(*sources.NinjaDataset)
		}
	}
	currency := ""
	if ninja != nil {
		currency = ninja.Currency
	}
	var out []Card
	for _, c := range metrics.ClientCards(kimai, ninja, time.Now(), metrics.CenterOf(sp.settings)) {
		out = append(out, Card{SpaceID: sp.ref.ID, SpaceName: sp.ref.Name, Currency: currency, ClientCard: c})
	}
	return out, nil
}

// hintsNaming returns the open hints whose text names the customer.
func hintsNaming(d *sql.DB, who *access.Principal, name string) ([]hints.View, error) {
	all, err := hints.Active(d, who, enums.SeverityInfo, nil, 0)
	if err != nil {
		return nil, err
	}
	key := strings.ToLower(name)
	var out []hints.View
	for _, h := range all {
		if strings.Contains(strings.ToLower(h.Title+" "+h.Why), key) {
			out = append(out, h)
		}
	}
	return out, nil
}
