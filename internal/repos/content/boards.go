package content

import (
	"database/sql"
	"errors"
	"strings"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
)

// ── Boards ──

const boardCols = `id, space_id, slug, name, position, theme_id, is_template, min_team_role,
	version, updated_at`

func scanBoardRow(row interface{ Scan(...any) error }) (*model.Board, error) {
	var b model.Board
	var updatedAt string
	var themeID sql.NullInt64
	var minRole sql.NullString

	err := row.Scan(
		&b.ID, &b.SpaceID, &b.Slug, &b.Name, &b.Position, &themeID, &b.IsTemplate, &minRole,
		&b.Version, &updatedAt,
	)
	if err != nil {
		return nil, err
	}
	if themeID.Valid {
		b.ThemeID = &themeID.Int64
	}
	if minRole.Valid {
		role := enums.TeamRole(minRole.String)
		b.MinTeamRole = &role
	}
	b.UpdatedAt, err = db.ParseTime(updatedAt)
	return &b, err
}

// loadSections fills Board.Sections (with Placements and Widget) for a board.
func loadSections(q db.Queryer, board *model.Board) error {
	rows, err := q.Query(
		"SELECT id, board_id, title, position, cols, size, sort, collapsed, area, span, row_span, color, mobile FROM sections WHERE board_id = ? ORDER BY position",
		board.ID,
	)
	if err != nil {
		return err
	}
	defer rows.Close()

	var sections []model.Section
	for rows.Next() {
		var sec model.Section
		var cols sql.NullInt64
		if err := rows.Scan(&sec.ID, &sec.BoardID, &sec.Title, &sec.Position, &cols,
			&sec.Size, &sec.Sort, &sec.Collapsed, &sec.Area, &sec.Span, &sec.Rows, &sec.Color, &sec.Mobile); err != nil {
			return err
		}
		if cols.Valid {
			n := int(cols.Int64)
			sec.Cols = &n
		}
		sections = append(sections, sec)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	for i := range sections {
		placements, err := loadPlacements(q, sections[i].ID)
		if err != nil {
			return err
		}
		sections[i].Placements = placements
	}
	board.Sections = sections
	return nil
}

func loadPlacements(q db.Queryer, sectionID int64) ([]model.Placement, error) {
	rows, err := q.Query(
		`SELECT p.id, p.section_id, p.widget_id, p.position, p.rows, p.cols,
			widgets.id, widgets.space_id, widgets.key, widgets.type, widgets.title, widgets.config,
			widgets.connection_id, widgets.min_team_role, widgets.version, widgets.updated_at
		FROM placements p JOIN widgets ON widgets.id = p.widget_id
		WHERE p.section_id = ? ORDER BY p.position`,
		sectionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []model.Placement
	for rows.Next() {
		var p model.Placement
		w, err := scanPlacementJoined(rows, &p)
		if err != nil {
			return nil, err
		}
		p.Widget = w
		out = append(out, p)
	}
	return out, rows.Err()
}

// scanPlacementJoined scans a placement row followed by a widget row (used
// by loadPlacements' JOIN query) into p and a returned Widget.
func scanPlacementJoined(rows *sql.Rows, p *model.Placement) (*model.Widget, error) {
	var w model.Widget
	var config, updatedAt string
	var connID sql.NullInt64
	var minRole sql.NullString

	err := rows.Scan(
		&p.ID, &p.SectionID, &p.WidgetID, &p.Position, &p.Rows, &p.Cols,
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
	if w.UpdatedAt, err = db.ParseTime(updatedAt); err != nil {
		return nil, err
	}
	return &w, nil
}

// Board returns a board with its sections/placements/widgets, or nil.
func Board(q db.Queryer, boardID int64) (*model.Board, error) {
	row := q.QueryRow("SELECT "+boardCols+" FROM boards WHERE id = ?", boardID)
	b, err := scanBoardRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := loadSections(q, b); err != nil {
		return nil, err
	}
	return b, nil
}

// BoardBySlug returns a board (with content) by its space-scoped slug.
func BoardBySlug(q db.Queryer, spaceID int64, slug string) (*model.Board, error) {
	row := q.QueryRow("SELECT "+boardCols+" FROM boards WHERE space_id = ? AND slug = ?", spaceID, slug)
	b, err := scanBoardRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := loadSections(q, b); err != nil {
		return nil, err
	}
	return b, nil
}

// Boards returns the boards of the given spaces (without content), ordered
// by position then id.
func Boards(q db.Queryer, spaceIDs []int64) ([]*model.Board, error) {
	if len(spaceIDs) == 0 {
		return nil, nil
	}
	query, args := inClause("SELECT "+boardCols+" FROM boards WHERE space_id IN (%s) ORDER BY position, id", spaceIDs)
	rows, err := q.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*model.Board
	for rows.Next() {
		b, err := scanBoardRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// AddBoard inserts a new board.
func AddBoard(q db.Queryer, b *model.Board) error {
	res, err := q.Exec(`INSERT INTO boards
		(space_id, slug, name, position, theme_id, is_template, min_team_role, version, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		b.SpaceID, b.Slug, b.Name, b.Position, b.ThemeID, b.IsTemplate, minRoleStr(b.MinTeamRole),
		b.Version, db.TimeStr(b.UpdatedAt),
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	b.ID = id
	return nil
}

// UpdateBoard writes back the board's own fields (not its sections).
func UpdateBoard(q db.Queryer, b *model.Board) error {
	_, err := q.Exec(`UPDATE boards SET
		name=?, position=?, theme_id=?, is_template=?, min_team_role=?, version=?, updated_at=?
		WHERE id=?`,
		b.Name, b.Position, b.ThemeID, b.IsTemplate, minRoleStr(b.MinTeamRole), b.Version,
		db.TimeStr(b.UpdatedAt), b.ID,
	)
	return err
}

// RemoveBoard deletes a board (cascades to sections/placements/overlays).
func RemoveBoard(q db.Queryer, boardID int64) error {
	_, err := q.Exec("DELETE FROM boards WHERE id = ?", boardID)
	return err
}

// Section returns a section by id (without placements), or nil.
func Section(q db.Queryer, sectionID int64) (*model.Section, error) {
	var sec model.Section
	var cols sql.NullInt64
	err := q.QueryRow(
		"SELECT id, board_id, title, position, cols, size, sort, collapsed, area, span, row_span, color, mobile FROM sections WHERE id = ?",
		sectionID,
	).Scan(&sec.ID, &sec.BoardID, &sec.Title, &sec.Position, &cols, &sec.Size, &sec.Sort,
		&sec.Collapsed, &sec.Area, &sec.Span, &sec.Rows, &sec.Color, &sec.Mobile)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if cols.Valid {
		n := int(cols.Int64)
		sec.Cols = &n
	}
	return &sec, nil
}

// AddSection inserts a new section.
func AddSection(q db.Queryer, sec *model.Section) error {
	res, err := q.Exec(
		"INSERT INTO sections (board_id, title, position, cols, size, sort, collapsed, area, span, row_span, color, mobile) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)",
		sec.BoardID, sec.Title, sec.Position, sec.Cols, sec.Size, sec.Sort, sec.Collapsed, sec.Area, sec.Span, sec.Rows, sec.Color, sec.Mobile,
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	sec.ID = id
	return nil
}

// UpdateSection writes back a section's own fields.
func UpdateSection(q db.Queryer, sec *model.Section) error {
	_, err := q.Exec(
		"UPDATE sections SET title=?, position=?, cols=?, size=?, sort=?, collapsed=?, area=?, span=?, row_span=?, color=?, mobile=? WHERE id=?",
		sec.Title, sec.Position, sec.Cols, sec.Size, sec.Sort, sec.Collapsed, sec.Area, sec.Span, sec.Rows, sec.Color, sec.Mobile, sec.ID,
	)
	return err
}

// RemoveSection deletes a section (cascades to placements).
func RemoveSection(q db.Queryer, sectionID int64) error {
	_, err := q.Exec("DELETE FROM sections WHERE id = ?", sectionID)
	return err
}

// Placement returns a placement with its widget and section, or nil.
func Placement(q db.Queryer, placementID int64) (*model.Placement, error) {
	var p model.Placement
	err := q.QueryRow(
		"SELECT id, section_id, widget_id, position, rows, cols FROM placements WHERE id = ?", placementID,
	).Scan(&p.ID, &p.SectionID, &p.WidgetID, &p.Position, &p.Rows, &p.Cols)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	w, err := Widget(q, p.WidgetID)
	if err != nil {
		return nil, err
	}
	p.Widget = w
	return &p, nil
}

// AddPlacement inserts a new placement.
func AddPlacement(q db.Queryer, p *model.Placement) error {
	res, err := q.Exec(
		"INSERT INTO placements (section_id, widget_id, position, rows, cols) VALUES (?,?,?,?,?)",
		p.SectionID, p.WidgetID, p.Position, max(p.Rows, 1), max(p.Cols, 1),
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	p.ID = id
	return nil
}

// UpdatePlacementPosition moves a placement, possibly to another section.
func UpdatePlacementPosition(q db.Queryer, placementID, sectionID int64, position int) error {
	_, err := q.Exec(
		"UPDATE placements SET section_id = ?, position = ? WHERE id = ?",
		sectionID, position, placementID,
	)
	return err
}

// UpdatePlacementRows sets how many grid rows a placed tile spans.
func UpdatePlacementRows(q db.Queryer, placementID int64, rows int) error {
	_, err := q.Exec("UPDATE placements SET rows = ? WHERE id = ?", rows, placementID)
	return err
}

// UpdatePlacementCols sets how many grid columns a placed tile spans.
func UpdatePlacementCols(q db.Queryer, placementID int64, cols int) error {
	_, err := q.Exec("UPDATE placements SET cols = ? WHERE id = ?", cols, placementID)
	return err
}

// RemovePlacement deletes a placement.
func RemovePlacement(q db.Queryer, placementID int64) error {
	_, err := q.Exec("DELETE FROM placements WHERE id = ?", placementID)
	return err
}

// Overlay returns one user's layout overlay for a board, or nil.
func Overlay(q db.Queryer, userID, boardID int64) (*model.Overlay, error) {
	var o model.Overlay
	var data string
	err := q.QueryRow(
		"SELECT id, user_id, board_id, data FROM overlays WHERE user_id = ? AND board_id = ?",
		userID, boardID,
	).Scan(&o.ID, &o.UserID, &o.BoardID, &data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	o.Data = map[string]any{}
	return &o, db.FromJSON(data, &o.Data)
}

// SetOverlay inserts or replaces one user's board overlay.
func SetOverlay(q db.Queryer, userID, boardID int64, data map[string]any) error {
	text, err := db.ToJSON(orEmpty(data))
	if err != nil {
		return err
	}
	existing, err := Overlay(q, userID, boardID)
	if err != nil {
		return err
	}
	if existing == nil {
		_, err := q.Exec(
			"INSERT INTO overlays (user_id, board_id, data) VALUES (?,?,?)", userID, boardID, text,
		)
		return err
	}
	_, err = q.Exec("UPDATE overlays SET data = ? WHERE id = ?", text, existing.ID)
	return err
}

// RemoveOverlay deletes one user's board overlay, if any.
func RemoveOverlay(q db.Queryer, userID, boardID int64) error {
	_, err := q.Exec("DELETE FROM overlays WHERE user_id = ? AND board_id = ?", userID, boardID)
	return err
}

// ── Revisions ──

// Revisions returns the revisions of one entity, newest first.
func Revisions(q db.Queryer, kind enums.RevisionKind, entityID int64) ([]*model.Revision, error) {
	rows, err := q.Query(
		"SELECT id, kind, entity_id, space_id, user_id, version, data, created_at FROM revisions "+
			"WHERE kind = ? AND entity_id = ? ORDER BY id DESC",
		kind, entityID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*model.Revision
	for rows.Next() {
		r, err := scanRevision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanRevision(row interface{ Scan(...any) error }) (*model.Revision, error) {
	var r model.Revision
	var data, createdAt string
	var userID sql.NullInt64

	err := row.Scan(&r.ID, &r.Kind, &r.EntityID, &r.SpaceID, &userID, &r.Version, &data, &createdAt)
	if err != nil {
		return nil, err
	}
	if userID.Valid {
		r.UserID = &userID.Int64
	}
	r.Data = map[string]any{}
	if err := db.FromJSON(data, &r.Data); err != nil {
		return nil, err
	}
	r.CreatedAt, err = db.ParseTime(createdAt)
	return &r, err
}

// RevisionByID returns one revision, or nil.
func RevisionByID(q db.Queryer, revID int64) (*model.Revision, error) {
	row := q.QueryRow(
		"SELECT id, kind, entity_id, space_id, user_id, version, data, created_at FROM revisions WHERE id = ?",
		revID,
	)
	r, err := scanRevision(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return r, err
}

// AddRevision inserts a new revision.
func AddRevision(q db.Queryer, r *model.Revision) error {
	data, err := db.ToJSON(orEmpty(r.Data))
	if err != nil {
		return err
	}
	res, err := q.Exec(
		"INSERT INTO revisions (kind, entity_id, space_id, user_id, version, data, created_at) VALUES (?,?,?,?,?,?,?)",
		r.Kind, r.EntityID, r.SpaceID, r.UserID, r.Version, data, db.TimeStr(r.CreatedAt),
	)
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	r.ID = id
	return nil
}

// PruneRevisions deletes all but the most recent revisionsKept revisions of
// one entity.
func PruneRevisions(q db.Queryer, kind enums.RevisionKind, entityID int64) error {
	all, err := Revisions(q, kind, entityID)
	if err != nil {
		return err
	}
	if len(all) <= revisionsKept {
		return nil
	}
	for _, old := range all[revisionsKept:] {
		if _, err := q.Exec("DELETE FROM revisions WHERE id = ?", old.ID); err != nil {
			return err
		}
	}
	return nil
}

// ── helpers ──

func minRoleStr(role *enums.TeamRole) any {
	if role == nil {
		return nil
	}
	return string(*role)
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// inClause builds "... IN (?,?,?)" for a slice of int64 ids and returns the
// matching arg list, so callers avoid ad-hoc string building.
func inClause(format string, ids []int64) (string, []any) {
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	return sprintfIn(format, placeholders), args
}

func sprintfIn(format string, placeholders []string) string {
	return strings.Replace(format, "%s", strings.Join(placeholders, ","), 1)
}

// PlacedIn counts the tiles placed on boards of the given spaces.
func PlacedIn(q db.Queryer, spaceIDs []int64) (int, error) {
	if len(spaceIDs) == 0 {
		return 0, nil
	}
	query, args := inClause(`SELECT COUNT(*) FROM placements p
		JOIN sections s ON s.id = p.section_id JOIN boards b ON b.id = s.board_id
		WHERE b.space_id IN (%s)`, spaceIDs)
	var n int
	err := q.QueryRow(query, args...).Scan(&n)
	return n, err
}
