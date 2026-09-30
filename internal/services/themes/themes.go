// Package themes handles design tokens per color mode, rendered to one
// stylesheet.
//
//	theme = {dark: {--bg-void: #141312, ...}, light: {...overrides}, customCSS}
//	css   = :root{dark} :root[data-theme=light]{light} @media(auto -> light)
//
// The token names of the Kante design system are the theme contract.
// Only Kante ships; users duplicate it and change values.
package themes

import (
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/repos/misc"
	"andon/internal/repos/users"
	"andon/internal/services/access"
	"andon/internal/services/util"
)

//go:embed builtin/kante/tokens.css builtin/kante/theme.json
var builtinFiles embed.FS

const (
	builtinSlug = "kante"
	// legacySlug is the built-in theme's slug before the design system was
	// named Kante; EnsureBuiltin renames that row.
	legacySlug     = "shrippen"
	contractVer    = 1
	defaultSetting = "theme_default"
	maxCSS         = 50_000
	maxZip         = 8 * 1024 * 1024
	zipFontDir     = "fonts/"
	aaText         = 4.5
)

// extraDark/extraLight are tokens the dashboard adds on top of the design
// system (components hardcode these).
var (
	extraDark  = map[string]string{"--nav-bg": "rgba(20,19,18,.92)", "--shadow": "rgba(0,0,0,.35)"}
	extraLight = map[string]string{"--nav-bg": "rgba(240,233,214,.92)", "--shadow": "rgba(60,56,54,.18)"}
)

var textPairs = [][2]string{
	{"--fg1", "--bg-void"}, {"--fg1", "--bg-panel"}, {"--fg2", "--bg-panel"},
	{"--fg0", "--bg-void"}, {"--blue", "--bg-void"}, {"--cyan", "--bg-void"}, {"--fg3", "--bg-panel"},
}

var (
	blockRe   = regexp.MustCompile(`(?s):root(\[data-theme="light"\])?\s*\{(.*?)\}`)
	declRe    = regexp.MustCompile(`(?i)(--[a-z0-9-]+)\s*:\s*([^;]+);`)
	commentRe = regexp.MustCompile(`(?s)/\*.*?\*/`)
	safeValue = regexp.MustCompile(`^[#a-zA-Z0-9 ,.'"()%+\-/]*$`)
	hexColor  = regexp.MustCompile(`(?i)^#([0-9a-f]{3}|[0-9a-f]{6})$`)
)

var forbidden = []string{"url(", "expression", "@import", "javascript:", "\\"}

// ErrTheme is a validation failure with a translatable message key.
type ErrTheme struct{ Key string }

func (e ErrTheme) Error() string { return e.Key }

var (
	ErrNotFound = util.ErrNotFound
	ErrDenied   = access.ErrDenied
)

// Mode is a color scheme.
type Mode string

const (
	ModeDark  Mode = "dark"
	ModeLight Mode = "light"
)

// Ref is a lightweight theme listing entry.
type Ref struct {
	ID       int64
	Slug     string
	Name     string
	Builtin  bool
	SpaceID  *int64
	CanEdit  bool
	Swatches []string // dark-mode colours of swatchTokens, e.g. "#141312"
}

// swatchTokens are the colours a theme list shows: ground, text, main
// action, then the semantic colours.
var swatchTokens = []string{"--bg-void", "--fg1", "--primary", "--cyan", "--aqua", "--yellow", "--orange", "--red"}

// swatches resolves swatchTokens in a theme's dark mode; references like
// "var(--yellow)" follow to their colour.
func swatches(theme *model.Theme) []string {
	base, _ := Contract()
	tokens := merge(base, stringMap(theme.Dark))
	out := make([]string, 0, len(swatchTokens))
	for _, name := range swatchTokens {
		v := tokens[name]
		for i := 0; i < len(swatchTokens) && strings.HasPrefix(v, "var("); i++ {
			v = tokens[strings.TrimSuffix(strings.TrimPrefix(v, "var("), ")")]
		}
		if hexColor.MatchString(v) {
			out = append(out, v)
		}
	}
	return out
}

// ContrastIssue is one text/background pair failing WCAG AA.
type ContrastIssue struct {
	Mode  Mode
	FG    string
	BG    string
	Ratio float64
}

// ── Parsing and rendering ──

// ParseCSS extracts :root {...} and :root[data-theme="light"] {...}
// declarations into (dark, light) token maps.
func ParseCSS(text string) (dark, light map[string]string) {
	text = commentRe.ReplaceAllString(text, "")
	dark, light = map[string]string{}, map[string]string{}
	for _, m := range blockRe.FindAllStringSubmatch(text, -1) {
		target := dark
		if m[1] != "" {
			target = light
		}
		for _, d := range declRe.FindAllStringSubmatch(m[2], -1) {
			target[d[1]] = strings.Join(strings.Fields(d[2]), " ")
		}
	}
	return dark, light
}

var (
	contractDark, contractLight map[string]string
)

// Contract returns the token names and default values: the builtin theme
// plus the dashboard's own extras. Computed once and cached.
func Contract() (map[string]string, map[string]string) {
	if contractDark == nil {
		raw, err := builtinFiles.ReadFile("builtin/kante/tokens.css")
		if err != nil {
			panic("themes: missing embedded Kante tokens.css: " + err.Error())
		}
		dark, light := ParseCSS(string(raw))
		contractDark, contractLight = merge(dark, extraDark), merge(light, extraLight)
	}
	return contractDark, contractLight
}

func merge(base, extra map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

func checkValue(name, value string) (string, error) {
	value = strings.Join(strings.Fields(value), " ")
	low := strings.ToLower(value)
	for _, bad := range forbidden {
		if strings.Contains(low, bad) {
			return "", ErrTheme{"theme.bad_value:" + name}
		}
	}
	if !safeValue.MatchString(value) {
		return "", ErrTheme{"theme.bad_value:" + name}
	}
	return value, nil
}

// CleanTokens keeps only tokens the contract knows, validated against
// injection (no url(), @import, backslashes, ...).
func CleanTokens(tokens map[string]any) (map[string]string, error) {
	known, _ := Contract()
	out := map[string]string{}
	for name, raw := range tokens {
		if _, ok := known[name]; !ok {
			continue
		}
		value, err := checkValue(name, fmt.Sprint(raw))
		if err != nil {
			return nil, err
		}
		out[name] = value
	}
	return out, nil
}

// Render turns one theme into its full stylesheet (dark root, light
// override, prefers-color-scheme fallback, then any custom CSS).
func Render(theme *model.Theme) string {
	baseDark, baseLight := Contract()
	dark := merge(baseDark, stringMap(theme.Dark))
	light := merge(baseLight, stringMap(theme.Light))
	dark = merge(dark, TextRoles(dark))
	light = merge(light, TextRoles(merge(dark, light)))

	var b strings.Builder
	b.WriteString(fontFaces(theme))
	writeBlock(&b, ":root", dark, "dark")
	b.WriteByte('\n')
	writeBlock(&b, `:root[data-theme="light"]`, light, "light")
	b.WriteString("\n@media (prefers-color-scheme: light){:root:not([data-theme]){")
	for _, k := range sortedKeys(light) {
		fmt.Fprintf(&b, "%s:%s;", k, light[k])
	}
	b.WriteString("color-scheme:light}}")
	if theme.CustomCSS != "" {
		b.WriteByte('\n')
		b.WriteString(theme.CustomCSS)
	}
	return b.String()
}

func writeBlock(b *strings.Builder, selector string, tokens map[string]string, scheme string) {
	b.WriteString(selector)
	b.WriteByte('{')
	for _, k := range sortedKeys(tokens) {
		fmt.Fprintf(b, "%s:%s;", k, tokens[k])
	}
	fmt.Fprintf(b, "color-scheme:%s}", scheme)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func stringMap(m map[string]any) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = fmt.Sprint(v)
	}
	return out
}

// ── Contrast (WCAG) ──

func luminance(hexColorStr string) float64 {
	v := strings.TrimPrefix(hexColorStr, "#")
	if len(v) == 3 {
		v = string([]byte{v[0], v[0], v[1], v[1], v[2], v[2]})
	}
	var channels [3]float64
	for i := 0; i < 3; i++ {
		n, _ := strconv.ParseInt(v[i*2:i*2+2], 16, 32)
		c := float64(n) / 255
		if c <= 0.03928 {
			channels[i] = c / 12.92
		} else {
			channels[i] = math.Pow((c+0.055)/1.055, 2.4)
		}
	}
	return 0.2126*channels[0] + 0.7152*channels[1] + 0.0722*channels[2]
}

// Ratio computes the WCAG contrast ratio between two hex colors.
func Ratio(fg, bg string) float64 {
	a, b := luminance(fg), luminance(bg)
	if a < b {
		a, b = b, a
	}
	return round2((a + 0.05) / (b + 0.05))
}

func round2(f float64) float64 { return math.Round(f*100) / 100 }

// ContrastIssues checks the text pairs the Kante contract cares about
// and returns every pair failing WCAG AA (4.5:1) in either mode.
func ContrastIssues(dark, light map[string]string) []ContrastIssue {
	baseDark, baseLight := Contract()
	var issues []ContrastIssue
	modes := []struct {
		mode   Mode
		tokens map[string]string
	}{
		{ModeDark, merge(baseDark, dark)},
		{ModeLight, merge(merge(baseDark, baseLight), light)},
	}
	for _, m := range modes {
		for _, pair := range textPairs {
			a, b := m.tokens[pair[0]], m.tokens[pair[1]]
			if !hexColor.MatchString(a) || !hexColor.MatchString(b) {
				continue
			}
			if r := Ratio(a, b); r < aaText {
				issues = append(issues, ContrastIssue{Mode: m.mode, FG: pair[0], BG: pair[1], Ratio: r})
			}
		}
	}
	return issues
}

// textRoles are status colours for text: the role's colour, mixed toward
// --fg1 just enough to reach AA on every text surface. Borders and fills
// keep the pure colour. The sources are roles (--warn, --danger), so a
// theme that repoints a role changes its text colour too.
var textRoles = map[string]string{"--ok-text": "--aqua", "--warn-text": "--warn", "--danger-text": "--danger"}

var textSurfaces = []string{"--bg-void", "--bg-panel", "--bg-hard", "--bg0"}

const (
	mixStep    = 0.05
	maxVarHops = 4
)

// TextRoles derives the text-role tokens for one mode's resolved tokens,
// e.g. light --aqua #427b58 → --ok-text #3b6d4e.
func TextRoles(tokens map[string]string) map[string]string {
	out := map[string]string{}
	fg := tokens["--fg1"]
	for role, src := range textRoles {
		out[role] = "var(" + src + ")"
		base := resolveVar(tokens, src)
		if !hexColor.MatchString(base) || !hexColor.MatchString(fg) {
			continue
		}
		for share := 0.0; share <= 1; share += mixStep {
			mixed := mixHex(base, fg, share)
			if passesOn(mixed, tokens) {
				out[role] = mixed
				break
			}
		}
	}
	return out
}

// resolveVar follows var(--x) references, e.g. --warn → var(--orange) →
// #fe8019. A cycle or a dangling reference returns the last value seen.
func resolveVar(tokens map[string]string, name string) string {
	v := tokens[name]
	for i := 0; i < maxVarHops && strings.HasPrefix(v, "var(") && strings.HasSuffix(v, ")"); i++ {
		v = tokens[strings.TrimSuffix(strings.TrimPrefix(v, "var("), ")")]
	}
	return v
}

func passesOn(color string, tokens map[string]string) bool {
	for _, s := range textSurfaces {
		bg := tokens[s]
		if hexColor.MatchString(bg) && Ratio(color, bg) < aaText {
			return false
		}
	}
	return true
}

// mixHex blends share of b into a, per sRGB channel.
func mixHex(a, b string, share float64) string {
	ca, cb := rgb(a), rgb(b)
	var out [3]int
	for i := range out {
		out[i] = int(math.Round(float64(ca[i])*(1-share) + float64(cb[i])*share))
	}
	return fmt.Sprintf("#%02x%02x%02x", out[0], out[1], out[2])
}

func rgb(hex string) [3]int64 {
	v := strings.TrimPrefix(hex, "#")
	if len(v) == 3 {
		v = string([]byte{v[0], v[0], v[1], v[1], v[2], v[2]})
	}
	var out [3]int64
	for i := range out {
		out[i], _ = strconv.ParseInt(v[i*2:i*2+2], 16, 32)
	}
	return out
}

// ── Builtin ──

// EnsureBuiltin creates or refreshes the shipped Kante theme from the
// embedded tokens.css, and returns its id.
func EnsureBuiltin(d *sql.DB) (int64, error) {
	var id int64
	err := db.WithTx(d, func(tx *sql.Tx) error {
		metaRaw, err := builtinFiles.ReadFile("builtin/kante/theme.json")
		if err != nil {
			return err
		}
		var meta struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(metaRaw, &meta); err != nil {
			return err
		}
		tokensRaw, err := builtinFiles.ReadFile("builtin/kante/tokens.css")
		if err != nil {
			return err
		}
		dark, light := ParseCSS(string(tokensRaw))
		digestInput, _ := json.Marshal([]map[string]string{dark, light})
		sum := sha256.Sum256(digestInput)
		digest := fmt.Sprintf("%x", sum)[:12]

		theme, err := builtinRow(tx)
		if err != nil {
			return err
		}
		if theme != nil && (theme.Slug != builtinSlug || theme.Name != meta.Name) {
			theme.Slug, theme.Name = builtinSlug, meta.Name
			if err := misc.SetThemeSlug(tx, theme.ID, builtinSlug); err != nil {
				return err
			}
			if err := misc.UpdateTheme(tx, theme); err != nil {
				return err
			}
		}
		if theme == nil {
			theme = &model.Theme{Slug: builtinSlug, Name: meta.Name, Builtin: true, Version: 1}
			if err := misc.AddTheme(tx, theme); err != nil {
				return err
			}
		}
		if theme.Digest != digest {
			theme.Dark = anyMap(merge(dark, extraDark))
			theme.Light = anyMap(merge(light, extraLight))
			theme.Contract = contractVer
			theme.Digest = digest
			theme.Version++
			if err := misc.UpdateTheme(tx, theme); err != nil {
				return err
			}
		}
		id = theme.ID
		return nil
	})
	return id, err
}

// builtinRow finds the built-in theme, also under its old slug.
func builtinRow(q db.Queryer) (*model.Theme, error) {
	theme, err := misc.BuiltinTheme(q, builtinSlug)
	if err != nil || theme != nil {
		return theme, err
	}
	return misc.BuiltinTheme(q, legacySlug)
}

func anyMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ── Selection ──

// Active resolves the theme that applies: board forces > personal choice >
// team default > instance default > Kante.
func Active(d *sql.DB, who *access.Principal, boardTheme *int64, spaceID *int64) (int64, error) {
	var id int64
	err := db.WithRead(d, func(tx *sql.Tx) error {
		var candidates []*int64
		candidates = append(candidates, boardTheme)
		if who != nil {
			u, err := users.Get(tx, who.UserID)
			if err != nil {
				return err
			}
			if u != nil {
				candidates = append(candidates, u.ThemeID)
			}
		}
		if spaceID != nil {
			space, err := content.Space(tx, *spaceID)
			if err != nil {
				return err
			}
			if space != nil && space.Kind == enums.SpaceTeam {
				if raw, ok := space.Settings["theme_id"]; ok {
					if n, ok := raw.(float64); ok {
						themeID := int64(n)
						candidates = append(candidates, &themeID)
					}
				}
			}
		}
		def, err := defaultThemeID(tx)
		if err != nil {
			return err
		}
		candidates = append(candidates, def)

		for _, c := range candidates {
			if c == nil || *c == 0 {
				continue
			}
			t, err := misc.Theme(tx, *c)
			if err != nil {
				return err
			}
			if t != nil {
				id = t.ID
				return nil
			}
		}
		builtin, err := misc.BuiltinTheme(tx, builtinSlug)
		if err != nil {
			return err
		}
		if builtin == nil {
			return errors.New("themes: Kante not seeded")
		}
		id = builtin.ID
		return nil
	})
	return id, err
}

// DefaultID returns the instance default theme id, or nil for Kante.
func DefaultID(q db.Queryer) (*int64, error) { return defaultThemeID(q) }

func defaultThemeID(q db.Queryer) (*int64, error) {
	setting, err := misc.Setting(q, defaultSetting)
	if err != nil {
		return nil, err
	}
	if raw, ok := setting["id"].(float64); ok {
		id := int64(raw)
		return &id, nil
	}
	return nil, nil
}

// Stylesheet returns a theme's rendered CSS and version (for cache
// busting); an unknown id falls back to Kante.
func Stylesheet(d *sql.DB, themeID int64) (string, int, error) {
	var css string
	var version int
	err := db.WithRead(d, func(tx *sql.Tx) error {
		theme, err := misc.Theme(tx, themeID)
		if err != nil {
			return err
		}
		if theme == nil {
			theme, err = misc.BuiltinTheme(tx, builtinSlug)
			if err != nil {
				return err
			}
			if theme == nil {
				return errors.New("themes: Kante not seeded")
			}
		}
		css, version = Render(theme), theme.Version
		return nil
	})
	return css, version, err
}
