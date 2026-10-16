package sources

// The release list of all shrippen projects, for Andon's own update
// hint (shrippen.github.io/overview/VERSIONS.md):
//
//	{"format": 1, "projects": {"andon": {"version": "0.7.0", "date": "2026-10-07",
//	  "url": "https://github.com/shrippen/andon/releases/tag/v0.7.0"}}}
//
// A plain GET without parameters or cookies: nothing names the user or
// the installation.

import (
	"context"
	"net/url"
	"strings"
	"time"

	"andon/internal/drivers/httpclient"
)

const (
	// VersionsURL is the list's only address.
	VersionsURL = "https://shrippen.github.io/versions.json"
	// versionsFormat is the only format Andon reads.
	versionsFormat = 1
	versionsWait   = 15 * time.Second
)

// OwnRelease is a project's latest release.
type OwnRelease struct {
	Version string `json:"version"`
	Date    string `json:"date"`
	URL     string `json:"url"`
}

// FetchOwnRelease reads project's latest release from the list. An unknown
// format, a missing project or a link that is not https is an error.
func FetchOwnRelease(ctx context.Context, project string) (OwnRelease, error) {
	raw, _, err := httpclient.GetJSON(ctx, VersionsURL, httpclient.Options{Timeout: versionsWait})
	if err != nil {
		return OwnRelease{}, err
	}
	return releaseOf(raw, project)
}

// releaseOf picks project out of the decoded list.
func releaseOf(raw any, project string) (OwnRelease, error) {
	list, _ := raw.(map[string]any)
	if format, _ := list["format"].(float64); format != versionsFormat {
		return OwnRelease{}, newSourceError("versions.json: unknown format")
	}
	projects, _ := list["projects"].(map[string]any)
	entry, ok := projects[project].(map[string]any)
	if !ok {
		return OwnRelease{}, newSourceError("versions.json: no project %s", project)
	}

	r := OwnRelease{Version: strings.TrimPrefix(asStr(entry["version"]), "v"), Date: asStr(entry["date"]), URL: asStr(entry["url"])}
	link, err := url.Parse(r.URL)
	if err != nil || link.Scheme != "https" || link.Host == "" {
		return OwnRelease{}, newSourceError("versions.json: %s has no https link", project)
	}
	if r.Version == "" {
		return OwnRelease{}, newSourceError("versions.json: %s has no version", project)
	}
	return r, nil
}
