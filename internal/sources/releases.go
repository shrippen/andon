package sources

// Releases of well-known self-hosted projects on GitHub, read when the
// updates dialog opens: which version came out when, and whether its notes
// mention a security fix.
//
//	params repos ["immich-app/immich", …] → ReleasesResult (newest first per repo)

import (
	"context"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"andon/internal/drivers/httpclient"
)

// releasesTTL: GitHub allows 60 unauthenticated calls an hour.
const releasesTTL = 6 * time.Hour

// releasesPerRepo is how many of a repo's latest releases are read.
const releasesPerRepo = 10

// githubAPI is GitHub's REST API.
var githubAPI = "https://api.github.com"

// Release is one published version of a project.
type Release struct {
	Repo, Tag, URL string
	Published      time.Time
	Security       bool // the notes mention a security fix or a CVE
}

// ReleasesResult lists releases by repo, newest first.
type ReleasesResult struct{ ByRepo map[string][]Release }

// securityNote finds a security fix in release notes.
var securityNote = regexp.MustCompile(`(?i)\bsecurity\b|\bCVE-\d{4}-\d+|\bGHSA-`)

var ReleasesSource = source{key: "github.releases", ttl: releasesTTL, fetch: fetchReleases}

// releasesParallel is how many repos are read at once.
const releasesParallel = 4

func fetchReleases(ctx context.Context, sctx Ctx) (any, error) {
	repos, _ := sctx.Params["repos"].([]string)
	out := &ReleasesResult{ByRepo: map[string][]Release{}}
	var mu sync.Mutex
	parallel(ctx, len(repos), releasesParallel, func(i int) {
		repo := repos[i]
		body, _, err := httpclient.GetJSON(ctx, githubAPI+"/repos/"+repo+"/releases", httpclient.Options{
			Params: url.Values{"per_page": {strconv.Itoa(releasesPerRepo)}}, Headers: map[string]string{"Accept": "application/vnd.github+json"}})
		if err != nil {
			return
		}
		list := parseReleases(repo, body)
		mu.Lock()
		out.ByRepo[strings.ToLower(repo)] = list
		mu.Unlock()
	})
	return out, nil
}

// parseReleases reads GitHub's release list, drafts left out.
func parseReleases(repo string, body any) []Release {
	var out []Release
	for _, raw := range asList(body) {
		m := asMap(raw)
		if asBool(m["draft"]) || len(out) == releasesPerRepo {
			continue
		}
		published, _ := time.Parse(time.RFC3339, asStr(m["published_at"]))
		out = append(out, Release{Repo: repo, Tag: asStr(m["tag_name"]), URL: asStr(m["html_url"]), Published: published,
			Security: securityNote.MatchString(asStr(m["body"]) + " " + asStr(m["name"]))})
	}
	return out
}

// FindRelease is a repo's release of a version ("v1.2.3", "1.2.3" and
// "version/1.2.3" match); ok is false when it is not among the latest.
func (r *ReleasesResult) FindRelease(repo, version string) (Release, bool) {
	want := versionCore(version)
	for _, rel := range r.ByRepo[strings.ToLower(repo)] {
		if want != "" && versionCore(rel.Tag) == want {
			return rel, true
		}
	}
	return Release{}, false
}

// versionCore strips a tag's prefix: "version/2025.8.3" → "2025.8.3".
func versionCore(tag string) string {
	if i := strings.LastIndex(tag, "/"); i >= 0 {
		tag = tag[i+1:]
	}
	return strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(tag), "v"), "V")
}

func init() { Register(ReleasesSource) }
