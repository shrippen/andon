package detailacts

// Actions that change something in a connected service. Seeing a board is
// not enough: the viewer must be allowed to use the tile's connection.

import (
	"context"
	"database/sql"
	"strconv"
	"time"

	"andon/internal/model"
	"andon/internal/outbound"
	"andon/internal/services/access"
	auditsvc "andon/internal/services/audit"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
)

// dnsPause is how long a dialog pauses a DNS filter.
const dnsPause = 10 * time.Minute

func init() {
	register("pihole", "pause", pauseDNS(outbound.DNSPihole))
	register("adguard", "pause", pauseDNS(outbound.DNSAdGuard))
	register("grocy", "shopping_add", grocyShopping)
	register("tandoor", "check", tandoorCheck)
}

// tandoorCheck checks an entry of Tandoor's shopping list off.
func tandoorCheck(ctx context.Context, d *sql.DB, who *access.Principal, c Call) error {
	id, err := strconv.ParseInt(c.Form.Get("id"), 10, 64)
	if err != nil || id <= 0 {
		return ErrBadMark
	}
	conn, target, err := useConnection(d, who, c.Widget)
	if err != nil {
		return err
	}
	if err := outbound.TandoorCheck(ctx, target, id); err != nil {
		return err
	}
	svcdata.Forget(conn.ID)
	return auditsvc.Log(d, &who.UserID, "tandoor.check", conn.Name, c.IP, nil)
}

// grocyShopping puts the products below their minimum on Grocy's
// shopping list.
func grocyShopping(ctx context.Context, d *sql.DB, who *access.Principal, c Call) error {
	conn, target, err := useConnection(d, who, c.Widget)
	if err != nil {
		return err
	}
	if err := outbound.GrocyAddMissing(ctx, target); err != nil {
		return err
	}
	svcdata.Forget(conn.ID)
	return auditsvc.Log(d, &who.UserID, "grocy.shopping_add", conn.Name, c.IP, nil)
}

// pauseDNS switches the tile's DNS filter off for ten minutes.
func pauseDNS(kind outbound.DNSKind) act {
	return func(ctx context.Context, d *sql.DB, who *access.Principal, c Call) error {
		conn, target, err := useConnection(d, who, c.Widget)
		if err != nil {
			return err
		}
		if err := outbound.DNSPause(ctx, target, kind, dnsPause); err != nil {
			return err
		}
		svcdata.Forget(conn.ID)
		return auditsvc.Log(d, &who.UserID, "dns.pause", conn.Name, c.IP, nil)
	}
}

// useConnection is the tile's connection as a write target, once the
// viewer may use it.
func useConnection(d *sql.DB, who *access.Principal, w *model.Widget) (*model.Connection, outbound.Target, error) {
	if w.ConnectionID == nil {
		return nil, outbound.Target{}, ErrBadMark
	}
	if _, err := connections.Get(d, who, *w.ConnectionID); err != nil {
		return nil, outbound.Target{}, err
	}
	conn, err := connections.ByID(d, *w.ConnectionID)
	if err != nil {
		return nil, outbound.Target{}, err
	}
	secret, err := svcdata.Secret(d, conn, who.UserID)
	if err != nil {
		return nil, outbound.Target{}, err
	}
	return conn, outbound.Target{URL: conn.URL, Token: secret, VerifyTLS: conn.VerifyTLS}, nil
}
