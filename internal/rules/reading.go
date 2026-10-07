package rules

// Reading (news connection):
//
//	cross.project_mentioned   a post links an own GitHub repo or KDE Store
//	                          entry, or names the entry in its title
//
// Repos match by link only: names like "website" are too common for a
// title. Store entries are product names ("Timecode Clock") and match
// the title as whole words too.

import (
	"regexp"
	"strconv"
	"strings"

	"andon/internal/enums"
	"andon/internal/sources"
)

var newsSvc = string(enums.ServiceNews)

// storeHosts serve KDE Store entries under /p/<id> (subdomains too).
var storeHosts = []string{"store.kde.org", "pling.com", "opendesktop.org", "kde-look.org"}

// minTitleName is the shortest store name matched in titles.
const minTitleName = 6

func init() {
	Register("cross.project_mentioned", Cross, nil, projectMentioned)
}

// ownProject is a repo or store entry with what finds it in a post.
type ownProject struct {
	name    string
	service string
	links   []string       // lower case, e.g. "github.com/studio/website"
	title   *regexp.Regexp // nil: links only
}

func projectMentioned(_ any, _ map[string]any, env Env) []Finding {
	news, ok := env.Datasets[newsSvc].(*sources.NewsDataset)
	if !ok {
		return nil
	}
	projects := ownProjects(env)

	var found []Finding
	for _, item := range news.Items {
		for _, p := range projects {
			if !p.mentionedIn(item) {
				continue
			}
			found = append(found, Finding{Fingerprint: p.name + "|" + item.Link, Severity: enums.SeverityInfo, Message: "cross.project_mentioned",
				Params:    map[string]any{"project": p.name, "title": item.Title, "site": siteParam(item), "points": item.Points, "comments": item.Comments},
				ActionURL: item.Link, ActionLabel: "open_in_" + newsSvc, Sources: []string{newsSvc, p.service}})
		}
	}
	return found
}

// ownProjects lists the GitHub owner's repos and the store entries.
func ownProjects(env Env) []ownProject {
	var out []ownProject
	if gh, ok := env.Datasets[githubSvc].(*sources.GitHubDataset); ok {
		prefix := strings.ToLower(gh.Owner) + "/"
		for _, r := range gh.Repos {
			name := strings.ToLower(r.Name)
			if gh.Owner != "" && !strings.HasPrefix(name, prefix) {
				continue // a repo only watched for its releases
			}
			out = append(out, ownProject{name: r.Name, service: githubSvc, links: []string{"github.com/" + name}})
		}
	}
	if store, ok := env.Datasets[kdestoreSvc].(*sources.KDEStoreDataset); ok {
		for _, it := range store.Items {
			p := ownProject{name: it.Name, service: kdestoreSvc}
			for _, h := range storeHosts {
				p.links = append(p.links, h+"/p/"+strconv.FormatInt(it.ID, 10))
			}
			if len([]rune(it.Name)) >= minTitleName {
				p.title = regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(it.Name) + `\b`)
			}
			out = append(out, p)
		}
	}
	return out
}

// mentionedIn: the item links the project or names it in its title.
func (p ownProject) mentionedIn(item sources.NewsItem) bool {
	for _, u := range []string{item.URL, item.Link} {
		u = strings.ToLower(u)
		for _, l := range p.links {
			if linksTo(u, l) {
				return true
			}
		}
	}
	return p.title != nil && p.title.MatchString(item.Title)
}

// linksTo: u contains target, ended by the URL's end or a separator, so
// "github.com/studio/web" does not match ".../website".
func linksTo(u, target string) bool {
	for start := 0; ; {
		i := strings.Index(u[start:], target)
		if i < 0 {
			return false
		}
		end := start + i + len(target)
		if end == len(u) || strings.ContainsRune("/?#", rune(u[end])) {
			return true
		}
		start = end
	}
}

// siteParam names where the post is: "Hacker News", "r/selfhosted", the
// YouTube channel.
func siteParam(item sources.NewsItem) any {
	switch {
	case item.Site == sources.SiteReddit:
		return "r/" + item.Feed
	case item.Feed != "":
		return item.Feed
	}
	return map[string]any{"$t": "reading.site." + item.Site}
}
