package metrics

// Release feeds against the running stacks (services/feeds): the update
// bench finds newer tags only for images whose project has a release
// feed in FreshRSS.
//
//	pinned image, no feed      ─► subscribe github.com/<owner>/<name>/releases.atom
//	feed, no image runs it     ─► unsubscribe (service gone)
//	feed without any release   ─► unsubscribe (wrong feed, project quiet)

import (
	"regexp"
	"slices"
	"strings"

	"andon/internal/sources"
)

// FeedAction is what a feed case proposes.
type FeedAction string

const (
	FeedAdd    FeedAction = "add"
	FeedUnused FeedAction = "unused"
	FeedQuiet  FeedAction = "quiet"
)

// FeedCase is one proposal.
type FeedCase struct {
	Key    string
	Action FeedAction
	Repo   string // "louislam/uptime-kuma"
	URL    string // the feed to subscribe, or the subscribed one
	Feed   string // stream id of a subscribed feed, "feed/150"
	Title  string
	Images []string // images running the project (add)
}

// releaseRepo reads the repo of a GitHub release feed's address.
var releaseRepo = regexp.MustCompile(`^https://github\.com/([^/]+/[^/]+)/(?:releases|tags)\.atom$`)

// FeedCases lists the proposals: missing feeds first, by repo.
func FeedCases(gitea *sources.GiteaDataset, feeds *sources.FreshRSSDataset) []FeedCase {
	if gitea == nil || feeds == nil {
		return nil
	}
	var images []string
	for _, s := range gitea.Stacks {
		for _, svc := range s.Services {
			if image, tag := splitTag(svc.Image); tag != "" && !strings.Contains(svc.Image, "${") && !slices.Contains(images, image) {
				images = append(images, image)
			}
		}
	}
	subscribed := map[string]sources.Feed{}
	for _, f := range feeds.Feeds {
		if m := releaseRepo.FindStringSubmatch(f.URL); m != nil {
			subscribed[strings.ToLower(m[1])] = f
		}
	}

	out := missingFeeds(images, subscribed)
	silent := map[string]bool{}
	for _, f := range feeds.Silent {
		silent[f.ID] = true
		out = append(out, FeedCase{Key: subKey("", "fs "+f.ID), Action: FeedQuiet, URL: f.URL, Feed: f.ID, Title: f.Title})
	}
	for repo, f := range subscribed {
		if silent[f.ID] || slices.ContainsFunc(images, func(image string) bool { return runs(repo, image) }) {
			continue
		}
		out = append(out, FeedCase{Key: subKey("", "fu "+f.ID), Action: FeedUnused, Repo: repo, URL: f.URL, Feed: f.ID, Title: f.Title})
	}
	slices.SortStableFunc(out, func(a, b FeedCase) int {
		if a.Action != b.Action {
			return strings.Compare(string(a.Action), string(b.Action))
		}
		return strings.Compare(a.Repo+a.Title, b.Repo+b.Title)
	})
	return out
}

// missingFeeds proposes a release feed per project running without one:
// the image's owner and name on GitHub, which the owner may correct.
// Official images and repackagers' have no guessable repo.
func missingFeeds(images []string, subscribed map[string]sources.Feed) []FeedCase {
	byRepo := map[string]*FeedCase{}
	var order []string
	for _, image := range images {
		if slices.ContainsFunc(mapKeys(subscribed), func(repo string) bool { return runs(repo, image) }) {
			continue
		}
		parts := strings.Split(strings.ToLower(image), "/")
		if len(parts) < 2 || repackagers[alnum(parts[len(parts)-2])] || parts[len(parts)-2] == "library" {
			continue
		}
		repo := parts[len(parts)-2] + "/" + parts[len(parts)-1]
		c := byRepo[repo]
		if c == nil {
			c = &FeedCase{Key: subKey("", "fa "+repo), Action: FeedAdd, Repo: repo, URL: "https://github.com/" + repo + "/releases.atom"}
			byRepo[repo] = c
			order = append(order, repo)
		}
		c.Images = append(c.Images, image)
	}
	out := make([]FeedCase, 0, len(order))
	for _, repo := range order {
		out = append(out, *byRepo[repo])
	}
	return out
}

// runs: the image belongs to the repo's project (releaseOf's rules), or
// to its owner ("fireflyiii/core" ~ firefly-iii/firefly-iii). The owner
// alone does not give a version (immich-app/postgres is no Immich), but
// it says the feed is in use.
func runs(repo, image string) bool {
	if _, ok := releaseOf([]sources.FeedRelease{{Repo: repo}}, image); ok {
		return true
	}
	parts := strings.Split(strings.ToLower(image), "/")
	owner, _, _ := strings.Cut(repo, "/")
	return len(parts) >= 2 && parts[len(parts)-2] != "library" && alnum(parts[len(parts)-2]) == alnum(owner)
}

func mapKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
