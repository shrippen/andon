package web

// What a connection test shows besides OK or failed:
//
//	failed ─► testNote: the cause in the user's words, a link to fix it
//	          (address, login, Admin → Netzwerk), the raw text as detail
//	green  ─► tileOffer: the service's templates not set up yet, one
//	          click puts one on the picked board (POST /boards/tile)

import (
	"andon/internal/enums"
	"andon/internal/services/access"
	"andon/internal/services/boards"
	"andon/internal/services/connections"
)

// offerMax caps the templates offered after a green test.
const offerMax = 3

// networkPath is the network part of the instance settings.
const networkPath = "/admin/settings#network"

// testNote explains a failed test. Text and Fix are catalog keys; Text
// is empty when the cause is unknown, Fix without FixURL is a hint
// without a link (only admins change the network rule).
type testNote struct {
	Text, Fix, FixURL, Raw string
}

// noteOf explains result for who; the links lead where who may fix it.
func noteOf(result connections.TestResult, conn connections.View, who *access.Principal) testNote {
	note := testNote{Raw: result.Message}
	if result.Ok || result.Cause == connections.CauseNone {
		return note
	}
	note.Text = "conn.cause_" + string(result.Cause)
	manage := conn.Right >= enums.RightManage

	switch result.Cause {
	case connections.CauseEgress:
		note.Fix = "conn.cause_ask_network"
		if who.IsAdmin() {
			note.Fix, note.FixURL = "conn.cause_fix_network", networkPath
		}
	case connections.CauseAuth, connections.CauseForbidden:
		note.Fix, note.FixURL = "conn.cause_fix_login", recordPath(conn.ID, tabAccess)
	default:
		if manage {
			note.Fix, note.FixURL = "conn.cause_fix_address", recordPath(conn.ID, tabSettings)
		}
	}
	return note
}

// tileOffer is what a green test offers: templates and the boards the
// caller may put them on, the start board first.
type tileOffer struct {
	Tiles  []boards.SuggestedTile
	Boards []boards.BoardRef
}

// offerFor builds the offer for a connection, nil if there is nothing to
// offer or no board to put it on.
func (d Deps) offerFor(ctx Ctx, connID int64) *tileOffer {
	tiles, err := boards.Fitting(d.DB, ctx.Who, connID, offerMax)
	if err != nil || len(tiles) == 0 {
		return nil
	}
	listed, err := boards.Listed(d.DB, ctx.Who)
	if err != nil {
		return nil
	}
	out := &tileOffer{Tiles: tiles, Boards: editable(listed)}
	if len(out.Boards) == 0 {
		return nil
	}
	return out
}
