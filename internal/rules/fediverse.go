package rules

// Fediverse:
//
//	fediverse.mentions          unread mentions of the account
//	cross.release_unannounced   an own GitHub release or KDE Store update
//	                            of the last "days" without an own post
//	                            that links or names it

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/sources"
)

var fediSvc = string(enums.ServiceFediverse)

const (
	// announceGrace is how long a release may wait for its post.
	announceGrace = 24 * time.Hour
	// announceEarly: a post may come shortly before the release.
	announceEarly = 2 * 24 * time.Hour
)

func init() {
	Register("fediverse.mentions", fediSvc, nil, on(fediMentions))
	Register("cross.release_unannounced", Cross, map[string]any{"days": 30.0}, releaseUnannounced)
}

func fediMentions(data *sources.FediverseDataset, _ map[string]any, _ Env) []Finding {
	unread := data.Unread("mention")
	if len(unread) == 0 {
		return nil
	}
	return []Finding{svcFinding(fediSvc, "fediverse.mentions", "mentions", "fediverse.mentions", enums.SeverityInfo, data.URL+"/notifications",
		map[string]any{"count": len(unread), "from": unread[0].From, "text": orDash(unread[0].Text)})}
}

// release is a published version of an own project and what finds its
// announcement.
type release struct {
	project, version string
	at               time.Time
	service          string
	links            []string       // lower case
	name             *regexp.Regexp // the project's name as words
}

func releaseUnannounced(_ any, cfg map[string]any, env Env) []Finding {
	fedi, ok := env.Datasets[fediSvc].(*sources.FediverseDataset)
	if !ok {
		return nil
	}
	now := time.Now().UTC()
	since := now.AddDate(0, 0, -int(cfgFloat(cfg, "days")))

	var found []Finding
	for _, r := range ownReleases(env) {
		if r.at.Before(since) || now.Sub(r.at) < announceGrace || r.announcedIn(fedi.Posts) {
			continue
		}
		found = append(found, Finding{Fingerprint: "unannounced:" + r.project + "@" + r.version, Severity: enums.SeverityInfo, Message: "cross.release_unannounced",
			Params:    map[string]any{"project": r.project, "version": orDash(r.version), "day": Day(r.at)},
			ActionURL: fedi.URL, ActionLabel: "open_in_" + fediSvc, Sources: []string{fediSvc, r.service}})
	}
	return found
}

// ownReleases are the owner's GitHub releases and the store entries'
// latest updates.
func ownReleases(env Env) []release {
	var out []release
	if gh, ok := env.Datasets[githubSvc].(*sources.GitHubDataset); ok {
		prefix := strings.ToLower(gh.Owner) + "/"
		for _, r := range gh.Repos {
			name := strings.ToLower(r.Name)
			if r.ReleasedAt.IsZero() || (gh.Owner != "" && !strings.HasPrefix(name, prefix)) {
				continue
			}
			_, short, _ := strings.Cut(r.Name, "/")
			out = append(out, release{project: r.Name, version: r.Release, at: r.ReleasedAt, service: githubSvc,
				links: []string{"github.com/" + name}, name: wordsOf(short)})
		}
	}
	if store, ok := env.Datasets[kdestoreSvc].(*sources.KDEStoreDataset); ok {
		for _, it := range store.Items {
			if it.Changed.IsZero() {
				continue
			}
			r := release{project: it.Name, version: it.Version, at: it.Changed, service: kdestoreSvc, name: wordsOf(it.Name)}
			for _, h := range storeHosts {
				r.links = append(r.links, h+"/p/"+strconv.FormatInt(it.ID, 10))
			}
			out = append(out, r)
		}
	}
	return out
}

// wordsOf matches a name as whole words, case aside.
func wordsOf(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(name) + `\b`)
}

// announcedIn: a post from shortly before the release on links or names
// it.
func (r release) announcedIn(posts []sources.FediPost) bool {
	for _, p := range posts {
		if p.At.Before(r.at.Add(-announceEarly)) {
			continue
		}
		text := strings.ToLower(p.Text + " " + p.Link)
		for _, l := range r.links {
			if linksTo(text, l) {
				return true
			}
		}
		if r.name.MatchString(p.Text) {
			return true
		}
	}
	return false
}
