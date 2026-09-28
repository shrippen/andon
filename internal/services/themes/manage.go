package themes

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/misc"
	"andon/internal/services/access"
	"andon/internal/services/audit"
	"andon/internal/services/util"
)

// ── Management ──

func right(q db.Queryer, who *access.Principal, theme *model.Theme) (enums.Right, error) {
	if theme.Builtin {
		return enums.RightUse, nil
	}
	space, err := access.SpaceOf(q, who, spaceIDOf(theme))
	if err != nil {
		return enums.RightNone, err
	}
	return access.Right(who, enums.ResourceTheme, theme.ID, space, nil), nil
}

func spaceIDOf(theme *model.Theme) int64 {
	if theme.SpaceID == nil {
		return 0
	}
	return *theme.SpaceID
}

// Listing returns the themes who may at least USE.
func Listing(d *sql.DB, who *access.Principal) ([]Ref, error) {
	var out []Ref
	err := db.WithTx(d, func(tx *sql.Tx) error {
		spaceIDs := make([]int64, 0, len(who.Spaces))
		for id := range who.Spaces {
			spaceIDs = append(spaceIDs, id)
		}
		all, err := misc.Themes(tx, spaceIDs)
		if err != nil {
			return err
		}
		for _, t := range all {
			granted, err := right(tx, who, t)
			if err != nil {
				return err
			}
			if granted < enums.RightUse {
				continue
			}
			out = append(out, Ref{ID: t.ID, Slug: t.Slug, Name: t.Name, Builtin: t.Builtin,
				SpaceID: t.SpaceID, CanEdit: granted >= enums.RightEdit})
		}
		return nil
	})
	return out, err
}

// Get returns a theme and the caller's right on it. Requires USE.
func Get(d *sql.DB, who *access.Principal, themeID int64) (*model.Theme, enums.Right, error) {
	var theme *model.Theme
	var granted enums.Right
	err := db.WithTx(d, func(tx *sql.Tx) error {
		t, err := misc.Theme(tx, themeID)
		if err != nil {
			return err
		}
		if t == nil {
			return ErrNotFound
		}
		g, err := right(tx, who, t)
		if err != nil {
			return err
		}
		if err := access.Need(g, enums.RightUse); err != nil {
			return err
		}
		theme, granted = t, g
		return nil
	})
	return theme, granted, err
}

// Duplicate copies a theme into a space the caller may edit.
func Duplicate(d *sql.DB, who *access.Principal, themeID, spaceID int64, name string) (int64, error) {
	var id int64
	err := db.WithTx(d, func(tx *sql.Tx) error {
		source, err := misc.Theme(tx, themeID)
		if err != nil {
			return err
		}
		if source == nil {
			return ErrNotFound
		}
		g, err := right(tx, who, source)
		if err != nil {
			return err
		}
		if err := access.Need(g, enums.RightUse); err != nil {
			return err
		}
		space, err := access.SpaceOf(tx, who, spaceID)
		if err != nil {
			return err
		}
		if err := access.Need(access.SpaceRight(who, space), enums.RightEdit); err != nil {
			return err
		}

		existing, err := misc.Themes(tx, []int64{spaceID})
		if err != nil {
			return err
		}
		taken := map[string]bool{}
		for _, t := range existing {
			if t.SpaceID != nil && *t.SpaceID == spaceID {
				taken[t.Slug] = true
			}
		}
		label := strings.TrimSpace(name)
		if label == "" {
			label = source.Name
		}
		customCSS := ""
		if who.IsAdmin() {
			customCSS = source.CustomCSS
		}
		sid := spaceID
		theme := &model.Theme{
			SpaceID: &sid, Slug: util.Unique(util.Slug(name, "theme"), taken), Name: label,
			Dark: copyAnyMap(source.Dark), Light: copyAnyMap(source.Light), CustomCSS: customCSS, Version: 1,
		}
		if err := misc.AddTheme(tx, theme); err != nil {
			return err
		}
		id = theme.ID
		return audit.Log(tx, &who.UserID, "theme.created", theme.Name, "", nil)
	})
	return id, err
}

func copyAnyMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Update validates and stores new tokens/custom CSS for a non-builtin
// theme. Custom CSS may only be set by admins. Returns any WCAG AA issues
// the new tokens introduce (not a hard error: colors still get saved).
func Update(d *sql.DB, who *access.Principal, themeID int64, name string, dark, light map[string]any, customCSS *string) ([]ContrastIssue, error) {
	cleanDark, err := CleanTokens(dark)
	if err != nil {
		return nil, err
	}
	cleanLight, err := CleanTokens(light)
	if err != nil {
		return nil, err
	}

	err = db.WithTx(d, func(tx *sql.Tx) error {
		theme, err := misc.Theme(tx, themeID)
		if err != nil {
			return err
		}
		if theme == nil || theme.Builtin {
			return ErrDenied
		}
		g, err := right(tx, who, theme)
		if err != nil {
			return err
		}
		if err := access.Need(g, enums.RightEdit); err != nil {
			return err
		}

		if n := strings.TrimSpace(name); n != "" {
			theme.Name = n
		}
		theme.Dark = anyMap(cleanDark)
		theme.Light = anyMap(cleanLight)
		if customCSS != nil {
			if !who.IsAdmin() {
				return ErrDenied
			}
			if len(*customCSS) > maxCSS || strings.Contains(strings.ToLower(*customCSS), "@import") {
				return ErrTheme{"theme.css_invalid"}
			}
			theme.CustomCSS = *customCSS
		}
		theme.Version++
		return misc.UpdateTheme(tx, theme)
	})
	if err != nil {
		return nil, err
	}
	return ContrastIssues(cleanDark, cleanLight), nil
}

// Delete removes a non-builtin theme and its shares. Requires MANAGE.
func Delete(d *sql.DB, who *access.Principal, themeID int64) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		theme, err := misc.Theme(tx, themeID)
		if err != nil || theme == nil || theme.Builtin {
			return err
		}
		g, err := right(tx, who, theme)
		if err != nil {
			return err
		}
		if err := access.Need(g, enums.RightManage); err != nil {
			return err
		}
		if err := misc.DropShares(tx, enums.ResourceTheme, theme.ID); err != nil {
			return err
		}
		if err := store().RemoveAll(themeKey(theme.ID)); err != nil {
			return err
		}
		return misc.RemoveTheme(tx, theme.ID)
	})
}

// SetDefault sets the instance-wide default theme. Admin only.
func SetDefault(d *sql.DB, who *access.Principal, themeID *int64) error {
	if !who.IsAdmin() {
		return ErrDenied
	}
	// nil: no instance default, the built-in theme applies.
	value := map[string]any{}
	if themeID != nil {
		value["id"] = *themeID
	}
	return db.WithTx(d, func(tx *sql.Tx) error {
		return misc.SetSetting(tx, defaultSetting, value)
	})
}

// ── Import / export ──

// ExportZip packages a theme as a zip: theme.json, tokens.css, and
// custom.css if the theme has one.
func ExportZip(d *sql.DB, who *access.Principal, themeID int64) (string, []byte, error) {
	theme, _, err := Get(d, who, themeID)
	if err != nil {
		return "", nil, err
	}

	meta := map[string]any{"name": theme.Name, "slug": theme.Slug, "contract": theme.Contract, "modes": []string{"dark", "light"}}
	metaJSON, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return "", nil, err
	}

	var tokens strings.Builder
	tokens.WriteString(":root{\n")
	for _, k := range sortedKeys(stringMap(theme.Dark)) {
		fmt.Fprintf(&tokens, "  %s: %s;\n", k, theme.Dark[k])
	}
	tokens.WriteString("}\n:root[data-theme=\"light\"]{\n")
	for _, k := range sortedKeys(stringMap(theme.Light)) {
		fmt.Fprintf(&tokens, "  %s: %s;\n", k, theme.Light[k])
	}
	tokens.WriteString("}\n")

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := writeZipFile(zw, "theme.json", metaJSON); err != nil {
		return "", nil, err
	}
	if err := writeZipFile(zw, "tokens.css", []byte(tokens.String())); err != nil {
		return "", nil, err
	}
	if theme.CustomCSS != "" {
		if err := writeZipFile(zw, "custom.css", []byte(theme.CustomCSS)); err != nil {
			return "", nil, err
		}
	}
	for _, name := range theme.Fonts {
		data, err := store().Get(themeKey(theme.ID), name)
		if err != nil {
			continue
		}
		if err := writeZipFile(zw, zipFontDir+name, data); err != nil {
			return "", nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return "", nil, err
	}
	return theme.Slug + ".zip", buf.Bytes(), nil
}

func writeZipFile(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// ImportZip creates a new theme in spaceID from an exported zip.
func ImportZip(d *sql.DB, who *access.Principal, spaceID int64, blob []byte) (int64, error) {
	if len(blob) > maxZip {
		return 0, ErrTheme{"theme.zip_too_large"}
	}
	zr, err := zip.NewReader(bytes.NewReader(blob), int64(len(blob)))
	if err != nil {
		return 0, ErrTheme{"theme.zip_invalid"}
	}

	var meta struct {
		Name string `json:"name"`
	}
	metaRaw, err := readZipFile(zr, "theme.json")
	if err != nil {
		return 0, ErrTheme{"theme.zip_invalid"}
	}
	if err := json.Unmarshal(metaRaw, &meta); err != nil {
		return 0, ErrTheme{"theme.zip_invalid"}
	}
	tokensRaw, err := readZipFile(zr, "tokens.css")
	if err != nil {
		return 0, ErrTheme{"theme.zip_invalid"}
	}
	dark, light := ParseCSS(string(tokensRaw))
	css := ""
	if raw, err := readZipFile(zr, "custom.css"); err == nil {
		css = string(raw)
	}
	if meta.Name == "" {
		meta.Name = "Theme"
	}

	builtinID, err := EnsureBuiltin(d)
	if err != nil {
		return 0, err
	}
	newID, err := Duplicate(d, who, builtinID, spaceID, meta.Name)
	if err != nil {
		return 0, err
	}

	darkAny, lightAny := anyMap(dark), anyMap(light)
	var cssPtr *string
	if who.IsAdmin() && css != "" {
		cssPtr = &css
	}
	if _, err := Update(d, who, newID, meta.Name, darkAny, lightAny, cssPtr); err != nil {
		return 0, err
	}
	if !who.IsAdmin() {
		return newID, nil
	}
	for _, f := range zr.File {
		name, ok := strings.CutPrefix(f.Name, zipFontDir)
		if !ok || name == "" {
			continue
		}
		data, err := readZipFile(zr, f.Name)
		if err != nil {
			return 0, ErrTheme{"theme.zip_invalid"}
		}
		if err := AddFont(d, who, newID, name, data); err != nil {
			return 0, err
		}
	}
	return newID, nil
}

func readZipFile(zr *zip.Reader, name string) ([]byte, error) {
	f, err := zr.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}
