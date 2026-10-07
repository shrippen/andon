package hints

import (
	"database/sql"

	"andon/internal/services/access"
	"andon/internal/services/accounts"
)

// Layout is how the hints page lists hints, kept per user:
//
//	User.Prefs["hints_layout"] = "grouped" | "single"
type Layout string

const (
	LayoutGrouped Layout = "grouped" // by rule; a rule with 2+ hints gets a head and bulk bar
	LayoutSingle  Layout = "single"  // one compact row per hint
)

const layoutKey = "hints_layout"

// ParseLayout reads a stored or posted layout; anything else is grouped.
func ParseLayout(s string) Layout {
	if Layout(s) == LayoutSingle {
		return LayoutSingle
	}
	return LayoutGrouped
}

// LoadLayout returns the viewer's layout of the hints page.
func LoadLayout(d *sql.DB, who *access.Principal) (Layout, error) {
	p, err := accounts.GetProfile(d, who)
	if err != nil {
		return LayoutGrouped, err
	}
	s, _ := p.Prefs[layoutKey].(string)
	return ParseLayout(s), nil
}

// SaveLayout keeps the viewer's layout of the hints page.
func SaveLayout(d *sql.DB, who *access.Principal, l Layout) error {
	return accounts.SetPref(d, who, layoutKey, string(ParseLayout(string(l))))
}
