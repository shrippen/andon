package connections

// Moving a connection to another server keeps the connection itself, and
// so everything bound to its id: tiles, links between services, history,
// notes on hints, webhook events.
//
//	edit form, new server ──► Update refuses (ErrMoveHost)
//	                           │
//	move page ──► Move: token for the new server? ──► test there ──► save
//	                    shared: entered anew or kept on purpose
//	                    personal: holders' logins pause, each releases theirs

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"andon/internal/caps"
	"andon/internal/crypto"
	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/audit"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
)

// ErrMoveHost: an edit must not point a connection at another server;
// that is a move (Move), which asks where the token goes.
var ErrMoveHost = errors.New("connection.use_move")

// SecretMove says what a shared token does on a move.
type SecretMove string

const (
	SecretNew  SecretMove = "new"  // a token entered with the move, or none
	SecretKeep SecretMove = "keep" // the stored token goes along on purpose
)

// Check says whether a move tests the new address first.
type Check string

const (
	CheckFirst Check = "check" // save only when the test passes
	CheckSkip  Check = "skip"  // save untested, e.g. the server is still down
)

// Move points a connection at another server. Requires MANAGE.
//
// A shared token goes there only when entered anew (secret) or kept on
// purpose (SecretKeep). A template's holders keep their logins, paused
// until each sends theirs to the new server; the mover's own goes along
// when it was tested there. With CheckFirst the new address is tested
// with the token it will get, and a failed test saves nothing: its result
// comes back instead.
func Move(ctx context.Context, d *sql.DB, who *access.Principal, connID int64, url, secret string,
	keep SecretMove, check Check) (TestResult, error) {
	defer svcdata.Forget(connID) // cached data belongs to the old server

	conn, err := movable(d, who, connID)
	if err != nil {
		return TestResult{}, err
	}
	newURL := strings.TrimRight(strings.TrimSpace(url), "/")
	login, err := moveLogin(d, who, conn, secret, keep)
	if err != nil {
		return TestResult{}, err
	}

	ok := TestResult{Ok: true, Message: "ok"}
	if check == CheckFirst {
		if ok = probe(ctx, conn, newURL, login); !ok.Ok {
			return ok, nil
		}
	}

	return ok, db.WithTx(d, func(tx *sql.Tx) error {
		return saveMove(tx, who, conn, newURL, secret, check)
	})
}

// movable reads a connection the caller may manage.
func movable(d *sql.DB, who *access.Principal, connID int64) (*model.Connection, error) {
	var conn *model.Connection
	err := db.WithRead(d, func(tx *sql.Tx) error {
		c, err := content.Connection(tx, connID)
		if err != nil {
			return err
		}
		if c == nil {
			return ErrNotFound
		}
		granted, err := rightOf(tx, who, c)
		if err != nil {
			return err
		}
		if err := access.Need(granted, enums.RightManage); err != nil {
			return err
		}
		conn = c
		return nil
	})
	return conn, err
}

// moveLogin is the login the new server will get: the entered one, the
// stored shared one when kept on purpose, or the mover's own on a
// template. A stored shared token neither kept nor replaced is refused.
func moveLogin(d *sql.DB, who *access.Principal, conn *model.Connection, secret string, keep SecretMove) (string, error) {
	if secret != "" {
		return secret, nil
	}
	if conn.CredentialMode == enums.CredentialPersonal {
		cred, err := content.Credential(d, conn.ID, model.UserHolder(who.UserID))
		if err != nil || cred == nil || cred.SecretEnc == nil {
			return "", err
		}
		return crypto.Decrypt(cred.SecretEnc, crypto.PurposeCredential)
	}
	if len(conn.SecretEnc) == 0 {
		return "", nil
	}
	if keep != SecretKeep {
		return "", ErrSecretForHost
	}
	return crypto.Decrypt(conn.SecretEnc, crypto.PurposeCredential)
}

// probe runs the service's test source against the new address, outside
// the cache: nothing of it belongs to the connection yet. A signed-in
// connection's grant is fetched only through the cache, so it passes
// untested.
func probe(ctx context.Context, conn *model.Connection, url, login string) TestResult {
	if login == sources.GrantMarker {
		return TestResult{Ok: true, Message: "ok"}
	}
	source, err := sources.Get(conn.Service + ".test")
	if err != nil {
		return TestResult{Message: err.Error()}
	}
	sctx := sources.Ctx{URL: url, Secret: login, VerifyTLS: conn.VerifyTLS, Options: conn.Options}
	out, err := source.Fetch(ctx, sctx)
	if err != nil {
		return TestResult{Message: err.Error(), Cause: CauseOf(err.Error())}
	}
	data, _ := out.(map[string]any)
	version, _ := data["version"].(string)
	set, _ := data[sources.TestCaps].(caps.Set)
	return TestResult{Ok: true, Message: "ok", Version: version, Gaps: set.Partial()}
}

// saveMove stores the new address. Shared: an entered token replaces the
// stored one, else it was kept on purpose. Template: the new revision
// pauses every holder's login; the mover's own stays current when the
// new server accepted it (tested) or they entered a new one.
func saveMove(tx *sql.Tx, who *access.Principal, conn *model.Connection, url, secret string, check Check) error {
	from := conn.URL
	conn.URL = url

	var enc []byte
	if secret != "" {
		var err error
		if enc, err = crypto.Encrypt(secret, crypto.PurposeCredential, nil); err != nil {
			return err
		}
	}
	if conn.CredentialMode == enums.CredentialPersonal {
		conn.Revision++
		if err := keepMoversLogin(tx, who, conn, enc, check); err != nil {
			return err
		}
	} else if enc != nil {
		conn.SecretEnc = enc
		conn.SecretAt = time.Now().UTC()
	}

	if err := content.UpdateConnection(tx, conn); err != nil {
		return err
	}
	return audit.Log(tx, &who.UserID, "connection.moved", conn.Name, "", map[string]any{"from": from, "to": url})
}

// keepMoversLogin renews the mover's own login for the new revision: an
// entered one, or the stored one the test just sent there.
func keepMoversLogin(tx *sql.Tx, who *access.Principal, conn *model.Connection, enc []byte, check Check) error {
	h := model.UserHolder(who.UserID)
	if enc == nil {
		held, err := content.Credential(tx, conn.ID, h)
		if err != nil || held == nil || held.SecretEnc == nil || check != CheckFirst {
			return err
		}
	}
	return content.SetCredential(tx, conn.ID, h, enc, conn.Revision, snapshotOf(conn))
}
