package web

import (
	"cmp"
	"slices"
	"strconv"

	"andon/internal/enums"
	"andon/internal/services/access"
)

// The settings side navigation: one group per level the viewer reaches,
// the same pages on each level, plus what only that level has.
//
//	Ich            Profil · Sicherheit · Benachrichtigungen ·
//	               Verbindungen · Startseite · Finanzen · Regeln · Wartung ·
//	               Code · Themes · Import
//	Team <name>    Mitglieder · Verbindungen · Startseite · … · Code
//	Instanz        Verbindungen · Startseite · … · Code ·
//	(admins)       Benutzer · Teams · Einstellungen · Betrieb · Audit-Log

// settingsGroup is one level: a catalog key, or a team's name.
type settingsGroup struct {
	Label string
	Team  string
	Links []settingsLink
}

// settingsLink is one page; Label is a catalog key.
type settingsLink struct {
	Label string
	Href  string
}

// settingsPages are the templates shown inside the settings frame.
var settingsPages = map[string]bool{
	"profile": true, "security": true, "notify": true,
	"connections": true, "space_settings": true, "space_code": true, "themes": true, "import": true,
	"teams": true, "team": true, "admin_users": true, "admin_settings": true, "admin_ops": true, "admin_audit": true,
}

// Space settings sections, each its own page and form.
const (
	sectionPage        = "page"
	sectionFinance     = "finance"
	sectionRules       = "rules"
	sectionMaintenance = "maintenance"
)

// spaceSections are the sections in the order the navigation lists them.
var spaceSections = []string{sectionPage, sectionFinance, sectionRules, sectionMaintenance}

func spacePath(id int64) string { return "/spaces/" + strconv.FormatInt(id, 10) }

// sectionPath is a space settings section's page.
func sectionPath(id int64, section string) string { return spacePath(id) + "/settings/" + section }

// spaceLinks are the pages every level has for its space.
func spaceLinks(id int64) []settingsLink {
	out := []settingsLink{{Label: "nav.connections", Href: spacePath(id) + "/connections"}}
	for _, s := range spaceSections {
		out = append(out, settingsLink{Label: "settings." + s, Href: sectionPath(id, s)})
	}
	return append(out, settingsLink{Label: "edit.code", Href: spacePath(id) + "/code"})
}

// settingsNav builds the groups for who: own space, each team, and the
// instance for admins.
func settingsNav(who *access.Principal) []settingsGroup {
	var out []settingsGroup
	if mine := access.Personal(who); mine != nil {
		links := []settingsLink{
			{Label: "nav.profile", Href: "/me/profile"},
			{Label: "nav.security", Href: "/me/security"},
			{Label: "nav.notify", Href: "/me/notify"},
		}
		links = append(links, spaceLinks(mine.ID)...)
		links = append(links, settingsLink{Label: "nav.themes", Href: "/themes"}, settingsLink{Label: "nav.import", Href: "/import"})
		out = append(out, settingsGroup{Label: "settings.level_me", Links: links})
	}

	var teams []access.SpaceRef
	for _, sp := range who.Spaces {
		if sp.Kind == enums.SpaceTeam && sp.TeamID != nil {
			teams = append(teams, sp)
		}
	}
	slices.SortFunc(teams, func(a, b access.SpaceRef) int { return cmp.Compare(a.Name, b.Name) })
	for _, sp := range teams {
		links := []settingsLink{{Label: "settings.members", Href: "/teams/" + strconv.FormatInt(*sp.TeamID, 10)}}
		out = append(out, settingsGroup{Team: sp.Name, Links: append(links, spaceLinks(sp.ID)...)})
	}

	if !who.IsAdmin() {
		return out
	}
	var links []settingsLink
	for _, sp := range who.Spaces {
		if sp.Kind == enums.SpaceInstance {
			links = spaceLinks(sp.ID)
		}
	}
	links = append(links,
		settingsLink{Label: "nav.admin_users", Href: "/admin/users"},
		settingsLink{Label: "nav.teams", Href: "/teams"},
		settingsLink{Label: "nav.admin_settings", Href: "/admin/settings"},
		settingsLink{Label: "nav.admin_ops", Href: "/admin/operations"},
		settingsLink{Label: "nav.audit", Href: "/admin/audit"},
	)
	return append(out, settingsGroup{Label: "settings.level_instance", Links: links})
}
