// Package content provides database access for spaces and their content:
// connections, widgets, boards, overlays, revisions.
package content

import (
	"database/sql"
	"errors"
	"sync"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
)

const revisionsKept = 50

// ── Spaces ──

const spaceCols = `id, kind, name, owner_user_id, team_id, settings, version`

func scanSpace(row interface{ Scan(...any) error }) (*model.Space, error) {
	var sp model.Space
	var ownerID, teamID sql.NullInt64
	var settings string

	err := row.Scan(&sp.ID, &sp.Kind, &sp.Name, &ownerID, &teamID, &settings, &sp.Version)
	if err != nil {
		return nil, err
	}
	if ownerID.Valid {
		sp.OwnerUserID = &ownerID.Int64
	}
	if teamID.Valid {
		sp.TeamID = &teamID.Int64
	}
	sp.Settings, err = decodeSettings(sp.ID, settings)
	return &sp, err
}

// settingsCache keeps each space's settings decoded: every tile fragment
// reads them, and decoding a homelab's 9 KB took 0.25 ms, a copy 0.04 ms.
// Keyed by the stored text, so a write shows at once.
var settingsCache sync.Map // space id → decodedSettings

type decodedSettings struct {
	raw string
	val map[string]any
}

// decodeSettings returns the space's settings as a map of its own:
// callers may change it without touching the cache.
func decodeSettings(spaceID int64, raw string) (map[string]any, error) {
	if c, ok := settingsCache.Load(spaceID); ok && c.(decodedSettings).raw == raw {
		return copyJSON(c.(decodedSettings).val), nil
	}
	val := map[string]any{}
	if err := db.FromJSON(raw, &val); err != nil {
		return nil, err
	}
	settingsCache.Store(spaceID, decodedSettings{raw: raw, val: val})
	return copyJSON(val), nil
}

// copyJSON deep-copies decoded JSON: maps and lists, the rest are values.
func copyJSON(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = copyValue(v)
	}
	return out
}

func copyValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return copyJSON(t)
	case []any:
		list := make([]any, len(t))
		for i, x := range t {
			list[i] = copyValue(x)
		}
		return list
	}
	return v
}

// Space returns a space by id, or nil.
func Space(q db.Queryer, spaceID int64) (*model.Space, error) {
	row := q.QueryRow("SELECT "+spaceCols+" FROM spaces WHERE id = ?", spaceID)
	sp, err := scanSpace(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return sp, err
}

// PersonalSpace returns one user's personal space, or nil.
func PersonalSpace(q db.Queryer, userID int64) (*model.Space, error) {
	row := q.QueryRow("SELECT "+spaceCols+" FROM spaces WHERE owner_user_id = ?", userID)
	sp, err := scanSpace(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return sp, err
}

// TeamSpace returns one team's space, or nil.
func TeamSpace(q db.Queryer, teamID int64) (*model.Space, error) {
	row := q.QueryRow("SELECT "+spaceCols+" FROM spaces WHERE team_id = ?", teamID)
	sp, err := scanSpace(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return sp, err
}

// InstanceSpace returns the single instance-wide space, or nil.
func InstanceSpace(q db.Queryer) (*model.Space, error) {
	row := q.QueryRow("SELECT "+spaceCols+" FROM spaces WHERE kind = ?", enums.SpaceInstance)
	sp, err := scanSpace(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return sp, err
}

// Spaces returns the spaces with the given ids.
func Spaces(q db.Queryer, ids []int64) ([]*model.Space, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	query, args := inClause("SELECT "+spaceCols+" FROM spaces WHERE id IN (%s)", ids)
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSpaces(rows)
}

// AllSpaces returns every space.
func AllSpaces(q db.Queryer) ([]*model.Space, error) {
	rows, err := q.Query("SELECT " + spaceCols + " FROM spaces")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanSpaces(rows)
}

func scanSpaces(rows *sql.Rows) ([]*model.Space, error) {
	var out []*model.Space
	for rows.Next() {
		sp, err := scanSpace(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sp)
	}
	return out, rows.Err()
}

// AddSpace inserts a new space.
func AddSpace(q db.Queryer, sp *model.Space) error {
	settings, err := db.ToJSON(orEmpty(sp.Settings))
	if err != nil {
		return err
	}
	res, err := q.Exec(
		"INSERT INTO spaces (kind, name, owner_user_id, team_id, settings, version) VALUES (?,?,?,?,?,?)",
		sp.Kind, sp.Name, sp.OwnerUserID, sp.TeamID, settings, sp.Version,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	sp.ID = id
	return nil
}

// UpdateSpaceSettings writes settings and bumps version.
func UpdateSpaceSettings(q db.Queryer, spaceID int64, settings map[string]any, version int) error {
	text, err := db.ToJSON(orEmpty(settings))
	if err != nil {
		return err
	}
	_, err = q.Exec("UPDATE spaces SET settings = ?, version = ? WHERE id = ?", text, version, spaceID)
	return err
}

// RenameSpace updates a space's display name (e.g. to follow a team rename).
func RenameSpace(q db.Queryer, spaceID int64, name string) error {
	_, err := q.Exec("UPDATE spaces SET name = ? WHERE id = ?", name, spaceID)
	return err
}

// RemoveSpace deletes a space (cascades to its connections/widgets/boards).
func RemoveSpace(q db.Queryer, spaceID int64) error {
	_, err := q.Exec("DELETE FROM spaces WHERE id = ?", spaceID)
	return err
}

// ── Connections ──

const connCols = `id, space_id, key, name, service, url, credential_mode, secret_enc,
	options, verify_tls, created_at, secret_at, secret_expires, daily_budget, revision`

func scanConnection(row interface{ Scan(...any) error }) (*model.Connection, error) {
	var c model.Connection
	var options, createdAt, secretAt string

	err := row.Scan(
		&c.ID, &c.SpaceID, &c.Key, &c.Name, &c.Service, &c.URL, &c.CredentialMode,
		&c.SecretEnc, &options, &c.VerifyTLS, &createdAt, &secretAt, &c.SecretExpires, &c.DailyBudget, &c.Revision,
	)
	if err != nil {
		return nil, err
	}
	if c.SecretAt, err = optTime(secretAt); err != nil {
		return nil, err
	}
	c.Options = map[string]any{}
	if err := db.FromJSON(options, &c.Options); err != nil {
		return nil, err
	}
	c.CreatedAt, err = db.ParseTime(createdAt)
	return &c, err
}

// optTime parses an optional db time ("" = zero).
func optTime(raw string) (time.Time, error) {
	if raw == "" {
		return time.Time{}, nil
	}
	return db.ParseTime(raw)
}

func optTimeStr(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return db.TimeStr(t)
}

// Connection returns a connection by id, or nil.
func Connection(q db.Queryer, connID int64) (*model.Connection, error) {
	row := q.QueryRow("SELECT "+connCols+" FROM connections WHERE id = ?", connID)
	c, err := scanConnection(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

// Connections returns the connections of the given spaces, ordered by name.
func Connections(q db.Queryer, spaceIDs []int64) ([]*model.Connection, error) {
	if len(spaceIDs) == 0 {
		return nil, nil
	}
	query, args := inClause("SELECT "+connCols+" FROM connections WHERE space_id IN (%s) ORDER BY name", spaceIDs)
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanConnections(rows)
}

// AllConnections returns every connection.
func AllConnections(q db.Queryer) ([]*model.Connection, error) {
	rows, err := q.Query("SELECT " + connCols + " FROM connections")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanConnections(rows)
}

func scanConnections(rows *sql.Rows) ([]*model.Connection, error) {
	var out []*model.Connection
	for rows.Next() {
		c, err := scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ConnectionByKey looks up a connection by its space-scoped key.
func ConnectionByKey(q db.Queryer, spaceID int64, key string) (*model.Connection, error) {
	row := q.QueryRow(
		"SELECT "+connCols+" FROM connections WHERE space_id = ? AND key = ?", spaceID, key,
	)
	c, err := scanConnection(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

// AddConnection inserts a new connection.
func AddConnection(q db.Queryer, c *model.Connection) error {
	options, err := db.ToJSON(orEmpty(c.Options))
	if err != nil {
		return err
	}
	res, err := q.Exec(`INSERT INTO connections
		(space_id, key, name, service, url, credential_mode, secret_enc, options, verify_tls, created_at,
		 secret_at, secret_expires, daily_budget, revision)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		c.SpaceID, c.Key, c.Name, c.Service, c.URL, c.CredentialMode, c.SecretEnc, options,
		c.VerifyTLS, db.TimeStr(c.CreatedAt), optTimeStr(c.SecretAt), c.SecretExpires, c.DailyBudget, max(c.Revision, 1),
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	c.ID = id
	return nil
}

// UpdateConnection writes back every mutable field.
func UpdateConnection(q db.Queryer, c *model.Connection) error {
	options, err := db.ToJSON(orEmpty(c.Options))
	if err != nil {
		return err
	}
	_, err = q.Exec(`UPDATE connections SET
		name=?, service=?, url=?, credential_mode=?, secret_enc=?, options=?, verify_tls=?,
		secret_at=?, secret_expires=?, daily_budget=?, revision=?
		WHERE id=?`,
		c.Name, c.Service, c.URL, c.CredentialMode, c.SecretEnc, options, c.VerifyTLS,
		optTimeStr(c.SecretAt), c.SecretExpires, c.DailyBudget, max(c.Revision, 1), c.ID,
	)
	return err
}

// RemoveConnection deletes a connection.
func RemoveConnection(q db.Queryer, connID int64) error {
	_, err := q.Exec("DELETE FROM connections WHERE id = ?", connID)
	return err
}

// credCols reads a credential; holder folds user_id and team_id into one
// model.Holder (team ids negative).
const credCols = `id, connection_id, COALESCE(user_id, -team_id), secret_enc, secret_at, revision, snapshot`

func scanCredential(row interface{ Scan(...any) error }) (*model.Credential, error) {
	var c model.Credential
	var secretAt, snapshot string
	if err := row.Scan(&c.ID, &c.ConnectionID, &c.Holder, &c.SecretEnc, &secretAt, &c.Revision, &snapshot); err != nil {
		return nil, err
	}
	var err error
	if c.SecretAt, err = optTime(secretAt); err != nil {
		return nil, err
	}
	c.Snapshot = map[string]any{}
	return &c, db.FromJSON(snapshot, &c.Snapshot)
}

// holderWhere selects one holder's row: "user_id = ?" or "team_id = ?".
func holderWhere(h model.Holder) (string, int64) {
	if h.Team() > 0 {
		return "team_id = ?", h.Team()
	}
	return "user_id = ?", h.User()
}

// Credential returns one holder's login to a connection, or nil.
func Credential(q db.Queryer, connID int64, h model.Holder) (*model.Credential, error) {
	where, id := holderWhere(h)
	row := q.QueryRow("SELECT "+credCols+" FROM credentials WHERE connection_id = ? AND "+where, connID, id)
	c, err := scanCredential(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

// Credentials returns every login to a connection.
func Credentials(q db.Queryer, connID int64) ([]*model.Credential, error) {
	rows, err := q.Query("SELECT "+credCols+" FROM credentials WHERE connection_id = ? ORDER BY id", connID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*model.Credential
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// CredentialHolders maps every connection to the holders of a login to
// it, in one query (the analysis run needs all).
func CredentialHolders(q db.Queryer) (map[int64][]model.Holder, error) {
	rows, err := q.Query("SELECT connection_id, COALESCE(user_id, -team_id) FROM credentials ORDER BY connection_id, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[int64][]model.Holder{}
	for rows.Next() {
		var connID int64
		var h model.Holder
		if err := rows.Scan(&connID, &h); err != nil {
			return nil, err
		}
		out[connID] = append(out[connID], h)
	}
	return out, rows.Err()
}

// SetCredential inserts or replaces one holder's login, activated for the
// template's revision and values (snapshot). A nil secretEnc keeps the
// stored secret.
func SetCredential(q db.Queryer, connID int64, h model.Holder, secretEnc []byte, revision int, snapshot map[string]any) error {
	snap, err := db.ToJSON(orEmpty(snapshot))
	if err != nil {
		return err
	}
	existing, err := Credential(q, connID, h)
	if err != nil {
		return err
	}
	now := db.TimeStr(time.Now().UTC())
	if existing != nil {
		if secretEnc == nil {
			_, err = q.Exec("UPDATE credentials SET revision = ?, snapshot = ? WHERE id = ?", revision, snap, existing.ID)
			return err
		}
		_, err = q.Exec("UPDATE credentials SET secret_enc = ?, secret_at = ?, revision = ?, snapshot = ? WHERE id = ?",
			secretEnc, now, revision, snap, existing.ID)
		return err
	}

	var userID, teamID *int64
	if t := h.Team(); t > 0 {
		teamID = &t
	} else {
		u := h.User()
		userID = &u
	}
	_, err = q.Exec(
		"INSERT INTO credentials (connection_id, user_id, team_id, secret_enc, secret_at, revision, snapshot) VALUES (?,?,?,?,?,?,?)",
		connID, userID, teamID, secretEnc, now, revision, snap,
	)
	return err
}

// ClearSecrets forgets every login's secret to a connection but keeps the
// rows, so their holders still see what changed.
func ClearSecrets(q db.Queryer, connID int64) error {
	_, err := q.Exec("UPDATE credentials SET secret_enc = NULL, secret_at = '' WHERE connection_id = ?", connID)
	return err
}

// RemoveCredential deletes one holder's login, if any.
func RemoveCredential(q db.Queryer, connID int64, h model.Holder) error {
	where, id := holderWhere(h)
	_, err := q.Exec("DELETE FROM credentials WHERE connection_id = ? AND "+where, connID, id)
	return err
}

// ── Widgets ──

const widgetCols = `id, space_id, key, type, title, config, connection_id, min_team_role,
	version, updated_at`

func scanWidget(row interface{ Scan(...any) error }) (*model.Widget, error) {
	var w model.Widget
	var config, updatedAt string
	var connID sql.NullInt64
	var minRole sql.NullString

	err := row.Scan(
		&w.ID, &w.SpaceID, &w.Key, &w.Type, &w.Title, &config, &connID, &minRole,
		&w.Version, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	w.Config = map[string]any{}
	if err := db.FromJSON(config, &w.Config); err != nil {
		return nil, err
	}
	if connID.Valid {
		w.ConnectionID = &connID.Int64
	}
	if minRole.Valid {
		role := enums.TeamRole(minRole.String)
		w.MinTeamRole = &role
	}
	w.UpdatedAt, err = db.ParseTime(updatedAt)
	return &w, err
}

// Widget returns a widget by id, or nil.
func Widget(q db.Queryer, widgetID int64) (*model.Widget, error) {
	row := q.QueryRow("SELECT "+widgetCols+" FROM widgets WHERE id = ?", widgetID)
	w, err := scanWidget(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return w, err
}

// Widgets returns the widgets of the given spaces, ordered by title.
func Widgets(q db.Queryer, spaceIDs []int64) ([]*model.Widget, error) {
	if len(spaceIDs) == 0 {
		return nil, nil
	}
	query, args := inClause("SELECT "+widgetCols+" FROM widgets WHERE space_id IN (%s) ORDER BY title", spaceIDs)
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWidgets(rows)
}

func scanWidgets(rows *sql.Rows) ([]*model.Widget, error) {
	var out []*model.Widget
	for rows.Next() {
		w, err := scanWidget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

// WidgetsOfTypes returns every widget of the given types, in all spaces.
func WidgetsOfTypes(q db.Queryer, types []string) ([]*model.Widget, error) {
	if len(types) == 0 {
		return nil, nil
	}
	query, args := inClause("SELECT "+widgetCols+" FROM widgets WHERE type IN (%s) ORDER BY id", types)
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWidgets(rows)
}

// SetWidgetConfig replaces a widget's config as is: no version bump,
// for rewriting stored data rather than a user's change.
func SetWidgetConfig(q db.Queryer, widgetID int64, config map[string]any) error {
	text, err := db.ToJSON(orEmpty(config))
	if err != nil {
		return err
	}
	_, err = q.Exec("UPDATE widgets SET config = ? WHERE id = ?", text, widgetID)
	return err
}

// WidgetByKey looks up a widget by its space-scoped key.
func WidgetByKey(q db.Queryer, spaceID int64, key string) (*model.Widget, error) {
	row := q.QueryRow("SELECT "+widgetCols+" FROM widgets WHERE space_id = ? AND key = ?", spaceID, key)
	w, err := scanWidget(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return w, err
}

// WidgetUses counts how many placements reference a widget.
func WidgetUses(q db.Queryer, widgetID int64) (int, error) {
	var n int
	err := q.QueryRow("SELECT COUNT(*) FROM placements WHERE widget_id = ?", widgetID).Scan(&n)
	return n, err
}

// AddWidget inserts a new widget.
func AddWidget(q db.Queryer, w *model.Widget) error {
	config, err := db.ToJSON(orEmpty(w.Config))
	if err != nil {
		return err
	}
	res, err := q.Exec(`INSERT INTO widgets
		(space_id, key, type, title, config, connection_id, min_team_role, version, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		w.SpaceID, w.Key, w.Type, w.Title, config, w.ConnectionID, minRoleStr(w.MinTeamRole),
		w.Version, db.TimeStr(w.UpdatedAt),
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	w.ID = id
	return nil
}

// UpdateWidget writes back every mutable field and bumps version/updated_at.
func UpdateWidget(q db.Queryer, w *model.Widget) error {
	config, err := db.ToJSON(orEmpty(w.Config))
	if err != nil {
		return err
	}
	_, err = q.Exec(`UPDATE widgets SET
		title=?, config=?, connection_id=?, min_team_role=?, version=?, updated_at=?
		WHERE id=?`,
		w.Title, config, w.ConnectionID, minRoleStr(w.MinTeamRole), w.Version,
		db.TimeStr(w.UpdatedAt), w.ID,
	)
	return err
}

// RemoveWidget deletes a widget (cascades to placements).
func RemoveWidget(q db.Queryer, widgetID int64) error {
	_, err := q.Exec("DELETE FROM widgets WHERE id = ?", widgetID)
	return err
}
