// Package connections manages connections to services and their
// credentials. A connection lives on one level and is fixed or a template:
//
//	level      fixed (shared)               template (personal)
//	instance   admins set the one login     each user, or a team, activates it
//	team       the team owner sets it       each member activates it
//	personal   the owner's own connection   –
//
// Activating stores the holder's login with the template's revision; an
// edit of the template raises the revision and pauses every activation
// until its holder activates it again (a new host also drops the login).
//
// Tokens are write-only: they are encrypted and never shown again.
package connections

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	neturl "net/url"
	"strings"
	"time"

	"andon/internal/crypto"
	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/repos/misc"
	"andon/internal/services/access"
	"andon/internal/services/audit"
	"andon/internal/services/hooks"
	"andon/internal/services/svcdata"
	"andon/internal/services/util"
)

const sharedLocationKey = "dawarich_shared"

// ErrLocationPersonal means Dawarich location data may not be shared while
// location sharing is disabled instance-wide.
var ErrLocationPersonal = errors.New("connections: location data must stay personal")

// ErrNotFound means the connection does not exist.
var ErrNotFound = util.ErrNotFound

// ErrSecretForHost: a connection moved to another host needs its token
// entered again; the stored one must not go there.
var ErrSecretForHost = errors.New("connection.secret_for_host")

// ErrNotTemplate: only a template takes activations, only an instance
// template a team's.
var ErrNotTemplate = errors.New("connection.not_template")

// TLS selects certificate verification for a connection.
type TLS string

const (
	TLSVerify TLS = "verify"
	TLSSkip   TLS = "skip"
)

// View is a connection as shown to one principal.
type View struct {
	ID        int64
	Key       string
	Name      string
	Service   enums.ServiceType
	URL       string
	Mode      enums.CredentialMode
	Level     enums.SpaceKind
	HasSecret bool
	HasMine   bool
	Paused    bool // the caller's activation predates the template's last edit
	VerifyTLS bool
	Options   map[string]any
	SpaceID   int64
	Right     enums.Right

	SecretAt      time.Time // shared token, or the caller's own; zero = unknown
	SecretExpires string
	DailyBudget   int
	Refresh       int // main query interval in minutes, 0 = automatic
	AutoRefresh   int // the service's own interval in minutes
	Pace          svcdata.Pace
	Health        Health
}

func rightOf(q db.Queryer, who *access.Principal, conn *model.Connection) (enums.Right, error) {
	space, err := access.SpaceOf(q, who, conn.SpaceID)
	if err != nil {
		return enums.RightNone, err
	}
	return access.Right(who, enums.ResourceConnection, conn.ID, space, nil), nil
}

func viewOf(q db.Queryer, who *access.Principal, conn *model.Connection, granted enums.Right) (View, error) {
	cred, err := content.Credential(q, conn.ID, model.UserHolder(who.UserID))
	if err != nil {
		return View{}, err
	}
	space, err := access.SpaceOf(q, who, conn.SpaceID)
	if err != nil {
		return View{}, err
	}
	var level enums.SpaceKind
	if space != nil {
		level = space.Kind
	}
	health, err := healthOf(q, conn.ID, time.Now().UTC())
	if err != nil {
		return View{}, err
	}
	secretAt := conn.SecretAt
	if conn.CredentialMode == enums.CredentialPersonal {
		secretAt = time.Time{}
		if cred != nil {
			secretAt = cred.SecretAt
		}
	}
	return View{
		ID: conn.ID, Key: conn.Key, Name: conn.Name, Service: enums.ServiceType(conn.Service), URL: conn.URL,
		Mode: conn.CredentialMode, Level: level, HasSecret: len(conn.SecretEnc) > 0, HasMine: cred != nil,
		Paused:    cred != nil && paused(conn, cred),
		VerifyTLS: conn.VerifyTLS, Options: conn.Options, SpaceID: conn.SpaceID, Right: granted,
		SecretAt: secretAt, SecretExpires: conn.SecretExpires, DailyBudget: conn.DailyBudget, Health: health,
		Refresh: conn.RefreshMinutes, AutoRefresh: svcdata.DefaultMinutes(enums.ServiceType(conn.Service)), Pace: svcdata.PaceOf(conn.ID),
	}, nil
}

// paused reports whether an activation waits for its holder after an
// edit of the template.
func paused(conn *model.Connection, cred *model.Credential) bool {
	return cred.SecretEnc == nil || cred.Revision < conn.Revision
}

// snapshotOf is what an activation remembers of its template, to show
// what changed later (never the secret).
func snapshotOf(conn *model.Connection) map[string]any {
	return map[string]any{"name": conn.Name, "url": conn.URL, "verify_tls": conn.VerifyTLS, "options": orEmpty(conn.Options)}
}

// Listing returns the connections visible to who with at least `minimum`
// right (default USE).
func Listing(d *sql.DB, who *access.Principal, minimum enums.Right) ([]View, error) {
	if minimum == enums.RightNone {
		minimum = enums.RightUse
	}
	var out []View
	err := db.WithRead(d, func(tx *sql.Tx) error {
		spaceIDs := make([]int64, 0, len(who.Spaces))
		for id := range who.Spaces {
			spaceIDs = append(spaceIDs, id)
		}
		found, err := content.Connections(tx, spaceIDs)
		if err != nil {
			return err
		}

		for _, conn := range found {
			granted, err := rightOf(tx, who, conn)
			if err != nil {
				return err
			}
			if granted < minimum {
				continue
			}
			v, err := viewOf(tx, who, conn, granted)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return nil
	})
	return out, err
}

// Writable checks that who may write through each connection to its
// service (book, link, upload, start a timer). Requires EDIT: USE only
// reads.
func Writable(d *sql.DB, who *access.Principal, connIDs ...int64) error {
	for _, id := range connIDs {
		v, err := Get(d, who, id)
		if err != nil {
			return err
		}
		if err := access.Need(v.Right, enums.RightEdit); err != nil {
			return err
		}
	}
	return nil
}

// Get returns one connection's view. Requires at least USE.
func Get(d *sql.DB, who *access.Principal, connID int64) (View, error) {
	var out View
	err := db.WithRead(d, func(tx *sql.Tx) error {
		conn, err := content.Connection(tx, connID)
		if err != nil {
			return err
		}
		if conn == nil {
			return ErrNotFound
		}
		granted, err := rightOf(tx, who, conn)
		if err != nil {
			return err
		}
		if err := access.Need(granted, enums.RightUse); err != nil {
			return err
		}
		out, err = viewOf(tx, who, conn, granted)
		return err
	})
	return out, err
}

// modeAt is the mode a connection may have on its level: a personal
// space holds only fixed connections.
func modeAt(space *access.SpaceRef, mode enums.CredentialMode) enums.CredentialMode {
	if space != nil && space.Kind == enums.SpacePersonal {
		return enums.CredentialShared
	}
	return mode
}

func checkLocationSharing(q db.Queryer, service enums.ServiceType, mode enums.CredentialMode, space *access.SpaceRef) error {
	if service != enums.ServiceDawarich || mode == enums.CredentialPersonal {
		return nil
	}
	if space != nil && space.Kind == enums.SpacePersonal {
		return nil
	}
	setting, err := misc.Setting(q, sharedLocationKey)
	if err != nil {
		return err
	}
	if allowed, _ := setting["allowed"].(bool); !allowed {
		return ErrLocationPersonal
	}
	return nil
}

// Create adds a new connection.
func Create(d *sql.DB, who *access.Principal, spaceID int64, service enums.ServiceType, name, url string,
	mode enums.CredentialMode, secret string, tls TLS, options map[string]any) (int64, error) {
	var id int64
	err := db.WithTx(d, func(tx *sql.Tx) error {
		space, err := access.SpaceOf(tx, who, spaceID)
		if err != nil {
			return err
		}
		need := enums.RightEdit
		if space != nil && space.Kind == enums.SpaceTeam {
			need = enums.RightManage
		}
		if err := access.Need(access.SpaceRight(who, space), need); err != nil {
			return err
		}
		mode = modeAt(space, mode)
		if err := checkLocationSharing(tx, service, mode, space); err != nil {
			return err
		}

		existing, err := content.Connections(tx, []int64{spaceID})
		if err != nil {
			return err
		}
		taken := map[string]bool{}
		for _, c := range existing {
			taken[c.Key] = true
		}

		label := strings.TrimSpace(name)
		if label == "" {
			label = string(service)
		}
		var secretEnc []byte
		if secret != "" {
			enc, err := crypto.Encrypt(secret, crypto.PurposeCredential, nil)
			if err != nil {
				return err
			}
			secretEnc = enc
		}
		// Personal: the token entered here is the creator's own, not shared.
		var ownEnc []byte
		if mode == enums.CredentialPersonal {
			ownEnc, secretEnc = secretEnc, nil
		}
		var secretAt time.Time
		if secretEnc != nil {
			secretAt = time.Now().UTC()
		}
		conn := &model.Connection{
			SecretAt: secretAt, Revision: 1,
			SpaceID: spaceID, Key: util.Unique(util.Slug(label, string(service)), taken), Name: label,
			Service: string(service), URL: strings.TrimRight(strings.TrimSpace(url), "/"),
			CredentialMode: mode, SecretEnc: secretEnc, VerifyTLS: tls == TLSVerify, Options: orEmpty(options),
			CreatedAt: time.Now().UTC(),
		}
		if err := content.AddConnection(tx, conn); err != nil {
			return err
		}
		id = conn.ID
		if ownEnc != nil {
			if err := content.SetCredential(tx, conn.ID, model.UserHolder(who.UserID), ownEnc, conn.Revision, snapshotOf(conn)); err != nil {
				return err
			}
		}
		return audit.Log(tx, &who.UserID, "connection.created", conn.Name, "", nil)
	})
	return id, err
}

// Update changes a connection's mutable fields. Requires MANAGE.
func Update(d *sql.DB, who *access.Principal, connID int64, name, url string, mode enums.CredentialMode,
	secret *string, tls TLS, options map[string]any) error {
	defer svcdata.Forget(connID) // cached data may be stale now

	return db.WithTx(d, func(tx *sql.Tx) error {
		conn, err := content.Connection(tx, connID)
		if err != nil {
			return err
		}
		if conn == nil {
			return ErrNotFound
		}
		granted, err := rightOf(tx, who, conn)
		if err != nil {
			return err
		}
		if err := access.Need(granted, enums.RightManage); err != nil {
			return err
		}
		space, err := access.SpaceOf(tx, who, conn.SpaceID)
		if err != nil {
			return err
		}
		mode = modeAt(space, mode)
		if err := checkLocationSharing(tx, enums.ServiceType(conn.Service), mode, space); err != nil {
			return err
		}
		before := snapshotOf(conn)

		if n := strings.TrimSpace(name); n != "" {
			conn.Name = n
		}
		newURL := strings.TrimRight(strings.TrimSpace(url), "/")
		given := secret != nil && *secret != ""
		if hostOf(newURL) != hostOf(conn.URL) {
			if err := forgetSecrets(tx, who, conn, mode, given); err != nil {
				return err
			}
		}
		conn.URL = newURL
		if err := keepEditorToken(tx, who, conn, mode); err != nil {
			return err
		}
		conn.CredentialMode = mode
		conn.VerifyTLS = tls == TLSVerify
		if options != nil {
			conn.Options = options
		}
		if conn.CredentialMode == enums.CredentialPersonal && changed(before, snapshotOf(conn)) {
			conn.Revision++
		}
		if secret != nil && *secret != "" {
			enc, err := crypto.Encrypt(*secret, crypto.PurposeCredential, nil)
			if err != nil {
				return err
			}
			if err := storeSecret(tx, who, conn, enc); err != nil {
				return err
			}
			if err := audit.Log(tx, &who.UserID, "connection.secret_changed", conn.Name, "", nil); err != nil {
				return err
			}
		}
		if err := content.UpdateConnection(tx, conn); err != nil {
			return err
		}
		return audit.Log(tx, &who.UserID, "connection.updated", conn.Name, "", nil)
	})
}

// hostOf is a URL's scheme and host: "https://kimai.lan".
func hostOf(raw string) string {
	u, err := neturl.Parse(raw)
	if err != nil {
		return raw
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}

// changed reports whether a template's activations must be renewed: any
// value they were activated for differs (name only labels it).
func changed(before, after map[string]any) bool {
	for _, k := range []string{"url", "verify_tls", "options"} {
		a, _ := json.Marshal(before[k])
		b, _ := json.Marshal(after[k])
		if string(a) != string(b) {
			return true
		}
	}
	return false
}

// forgetSecrets: a connection moved to another host must not send the
// stored tokens there. A shared token has to be entered again (given);
// the holders' own tokens are dropped, each enters theirs anew. Their
// activations stay, so they still see what changed.
func forgetSecrets(tx *sql.Tx, who *access.Principal, conn *model.Connection, mode enums.CredentialMode, given bool) error {
	if mode == enums.CredentialShared && len(conn.SecretEnc) > 0 && !given {
		return ErrSecretForHost
	}
	conn.SecretEnc = nil
	if err := content.ClearSecrets(tx, conn.ID); err != nil {
		return err
	}
	return audit.Log(tx, &who.UserID, "connection.host_changed", conn.Name, "", nil)
}

// storeSecret keeps a token where the connection's mode reads it: on the
// connection when shared, as the editor's own token when personal.
func storeSecret(tx *sql.Tx, who *access.Principal, conn *model.Connection, enc []byte) error {
	if conn.CredentialMode == enums.CredentialPersonal {
		return content.SetCredential(tx, conn.ID, model.UserHolder(who.UserID), enc, conn.Revision, snapshotOf(conn))
	}
	conn.SecretEnc = enc
	conn.SecretAt = time.Now().UTC()
	return nil
}

// keepEditorToken: a shared connection switched to personal would leave
// its editor without a token; the shared one becomes theirs unless they
// already have their own.
func keepEditorToken(tx *sql.Tx, who *access.Principal, conn *model.Connection, mode enums.CredentialMode) error {
	if mode != enums.CredentialPersonal || conn.CredentialMode == enums.CredentialPersonal || len(conn.SecretEnc) == 0 {
		return nil
	}
	own, err := content.Credential(tx, conn.ID, model.UserHolder(who.UserID))
	if err != nil || own != nil {
		return err
	}
	return content.SetCredential(tx, conn.ID, model.UserHolder(who.UserID), conn.SecretEnc, conn.Revision, snapshotOf(conn))
}

// SetOptions replaces a connection's service-specific options (e.g. the
// Dawarich area -> Kimai customer mapping). Requires MANAGE.
func SetOptions(d *sql.DB, who *access.Principal, connID int64, options map[string]any) error {
	defer svcdata.Forget(connID) // cached data may be stale now

	return db.WithTx(d, func(tx *sql.Tx) error {
		conn, err := content.Connection(tx, connID)
		if err != nil {
			return err
		}
		if conn == nil {
			return ErrNotFound
		}
		granted, err := rightOf(tx, who, conn)
		if err != nil {
			return err
		}
		if err := access.Need(granted, enums.RightManage); err != nil {
			return err
		}
		conn.Options = options
		if err := content.UpdateConnection(tx, conn); err != nil {
			return err
		}
		return audit.Log(tx, &who.UserID, "connection.options", conn.Name, "", nil)
	})
}

// Delete removes a connection and its shares. Requires MANAGE.
func Delete(d *sql.DB, who *access.Principal, connID int64) error {
	defer svcdata.Forget(connID) // cached data may be stale now

	return db.WithTx(d, func(tx *sql.Tx) error {
		conn, err := content.Connection(tx, connID)
		if err != nil || conn == nil {
			return err
		}
		granted, err := rightOf(tx, who, conn)
		if err != nil {
			return err
		}
		if err := access.Need(granted, enums.RightManage); err != nil {
			return err
		}
		if err := misc.DropShares(tx, enums.ResourceConnection, conn.ID); err != nil {
			return err
		}
		if err := audit.Log(tx, &who.UserID, "connection.deleted", conn.Name, "", nil); err != nil {
			return err
		}
		return content.RemoveConnection(tx, conn.ID)
	})
}

// RotateHook gives a push connection a new webhook URL, e.g. after the
// old one leaked. Requires MANAGE.
func RotateHook(d *sql.DB, who *access.Principal, connID int64) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		conn, err := content.Connection(tx, connID)
		if err != nil || conn == nil {
			return err
		}
		granted, err := rightOf(tx, who, conn)
		if err != nil {
			return err
		}
		if err := access.Need(granted, enums.RightManage); err != nil {
			return err
		}
		if err := hooks.Rotate(tx, conn.ID); err != nil {
			return err
		}
		return audit.Log(tx, &who.UserID, "connection.hook_rotated", conn.Name, "", nil)
	})
}

// Activate stores a holder's login to a template, for its current
// revision. The holder is the caller, or a team the caller owns, which
// activates an instance template for its team space. An empty secret
// renews the activation with the login stored before (after an edit of
// the template on the same host).
func Activate(d *sql.DB, who *access.Principal, connID int64, h model.Holder, secret string) error {
	defer svcdata.Forget(connID) // cached data may be stale now

	return db.WithTx(d, func(tx *sql.Tx) error {
		conn, err := activatable(tx, who, connID, h)
		if err != nil {
			return err
		}

		var enc []byte
		if secret != "" {
			if enc, err = crypto.Encrypt(secret, crypto.PurposeCredential, nil); err != nil {
				return err
			}
		} else {
			held, err := content.Credential(tx, conn.ID, h)
			if err != nil {
				return err
			}
			if held == nil || held.SecretEnc == nil {
				return svcdata.ErrMissingCredential
			}
		}
		if err := content.SetCredential(tx, conn.ID, h, enc, conn.Revision, snapshotOf(conn)); err != nil {
			return err
		}
		return audit.Log(tx, &who.UserID, "credential.set", conn.Name, "", nil)
	})
}

// activatable checks that who may hold or set h's login to connID.
func activatable(tx *sql.Tx, who *access.Principal, connID int64, h model.Holder) (*model.Connection, error) {
	conn, err := content.Connection(tx, connID)
	if err != nil {
		return nil, err
	}
	if conn == nil {
		return nil, ErrNotFound
	}
	granted, err := rightOf(tx, who, conn)
	if err != nil {
		return nil, err
	}
	if err := access.Need(granted, enums.RightView); err != nil {
		return nil, err
	}
	if conn.CredentialMode != enums.CredentialPersonal {
		return nil, ErrNotTemplate
	}
	if h.User() > 0 {
		if h.User() != who.UserID {
			return nil, access.ErrDenied
		}
		return conn, nil
	}

	space, err := access.SpaceOf(tx, who, conn.SpaceID)
	if err != nil {
		return nil, err
	}
	if space == nil || space.Kind != enums.SpaceInstance {
		return nil, ErrNotTemplate
	}
	if who.Teams[h.Team()] != enums.TeamOwner {
		return nil, access.ErrDenied
	}
	return conn, nil
}

// Deactivate removes a holder's login to a template.
func Deactivate(d *sql.DB, who *access.Principal, connID int64, h model.Holder) error {
	defer svcdata.Forget(connID) // cached data may be stale now

	return db.WithTx(d, func(tx *sql.Tx) error {
		if _, err := activatable(tx, who, connID, h); err != nil {
			return err
		}
		if err := content.RemoveGrant(tx, connID, int64(h)); err != nil {
			return err
		}
		return content.RemoveCredential(tx, connID, h)
	})
}

// PersonalNeeded returns visible connections that need the caller's own
// personal credentials.
func PersonalNeeded(d *sql.DB, who *access.Principal) ([]View, error) {
	all, err := Listing(d, who, enums.RightView)
	if err != nil {
		return nil, err
	}
	var out []View
	for _, c := range all {
		if c.Mode == enums.CredentialPersonal {
			out = append(out, c)
		}
	}
	return out, nil
}

// TestResult is the outcome of a live connection test.
type TestResult struct {
	Ok      bool
	Message string
	Version string
}

// Test calls the service's test source (a version check) with the stored
// credentials. Requires USE.
func Test(ctx context.Context, d *sql.DB, who *access.Principal, connID int64) (TestResult, error) {
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
		if err := access.Need(granted, enums.RightUse); err != nil {
			return err
		}
		conn = c
		return nil
	})
	if err != nil {
		return TestResult{}, err
	}

	result, err := svcdata.Get(ctx, d, conn.Service+".test", nil, conn, model.UserHolder(who.UserID), svcdata.Force)
	if err != nil {
		if errors.Is(err, svcdata.ErrMissingCredential) {
			return TestResult{Ok: false, Message: "credential.missing"}, nil
		}
		if errors.Is(err, svcdata.ErrTemplateChanged) {
			return TestResult{Ok: false, Message: "credential.paused"}, nil
		}
		return TestResult{}, err
	}
	if !result.Ok() {
		msg := result.Error
		if msg == "" {
			msg = "error"
		}
		return TestResult{Ok: false, Message: msg}, nil
	}
	version, _ := result.Data.(map[string]any)["version"].(string)
	return TestResult{Ok: true, Message: "ok", Version: version}, nil
}

// ByID returns a raw connection for internal jobs (rules). No access
// check: callers are jobs, not requests on a user's behalf.
func ByID(d *sql.DB, connID int64) (*model.Connection, error) {
	var conn *model.Connection
	err := db.WithRead(d, func(tx *sql.Tx) error {
		var err error
		conn, err = content.Connection(tx, connID)
		return err
	})
	return conn, err
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// HookURL is a connection's webhook address, "" for a service that sends
// none. The URL is its own secret, so it needs MANAGE, as rotating does.
func HookURL(d *sql.DB, who *access.Principal, connID int64, baseURL string) (string, error) {
	var out string
	err := db.WithRead(d, func(tx *sql.Tx) error {
		conn, err := content.Connection(tx, connID)
		if err != nil {
			return err
		}
		if conn == nil {
			return ErrNotFound
		}
		granted, err := rightOf(tx, who, conn)
		if err != nil {
			return err
		}
		if err := access.Need(granted, enums.RightManage); err != nil {
			return err
		}
		if !hooks.Accepts(enums.ServiceType(conn.Service)) {
			return nil
		}
		out, err = hooks.URL(tx, baseURL, conn.ID)
		return err
	})
	return out, err
}
