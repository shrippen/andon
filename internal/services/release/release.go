// Package release tells admins when a newer Andon is out: the Docker
// image says nothing by itself on `docker pull`.
//
//	daily job ──GET versions.json──► data/release.json ──► Pending ──► callout (admins)
//	                                                          ▲
//	                                   Hide: until the next version
//
// The server asks, never the browser (the CSP stays connect-src 'self'):
// at most once a day, silent on failure, off in demo mode and when an
// admin switches it off (ANDON_UPDATE_CHECK=0). Rules for every client:
// shrippen.github.io/overview/VERSIONS.md.
package release

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"andon/internal/services/about"
	"andon/internal/services/access"
	"andon/internal/services/workbench"
	"andon/internal/sources"
)

const (
	// JobName is the daily check in the scheduler.
	JobName = "release"
	// Interval is how often the list is fetched at most.
	Interval = 24 * time.Hour

	// project is Andon's key in versions.json.
	project = "andon"
	// cacheName is the last answer in the data folder.
	cacheName = "release.json"
	// hiddenKey keeps the version an admin hid, in their prefs.
	hiddenKey = "release_hidden"
)

// Release is the newer version the callout names.
type Release = sources.OwnRelease

// fetch reads the latest release, running names this build; variables
// for tests.
var (
	fetch   = sources.FetchOwnRelease
	running = about.Version
)

// Check fetches the latest release into dir once the last try is a day
// old. A failed try counts too (the old answer stays, its time moves):
// at most one request a day, even across restarts. Errors stay in the
// job's log, the page shows nothing.
func Check(ctx context.Context, dir string, now time.Time) error {
	path := filepath.Join(dir, cacheName)
	if info, err := os.Stat(path); err == nil && now.Sub(info.ModTime()) < Interval {
		return nil
	}

	r, err := fetch(ctx, project)
	if err != nil {
		return errors.Join(err, tried(path, now))
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0o600)
}

// Pending is the release to announce to who: newer than the running
// build and not hidden by them. Only admins get one.
func Pending(d *sql.DB, who *access.Principal, dir string) (Release, bool) {
	if who == nil || !who.IsAdmin() {
		return Release{}, false
	}
	r, ok := cached(dir)
	if !ok || !Newer(r.Version, running()) {
		return Release{}, false
	}
	h, err := workbench.LoadState[hidden](d, who, hiddenKey)
	if err != nil || h.Version == r.Version {
		return Release{}, false
	}
	return r, true
}

// Hide stops announcing version to who; a later one shows again.
func Hide(d *sql.DB, who *access.Principal, version string) error {
	if !who.IsAdmin() {
		return access.ErrDenied
	}
	return workbench.SaveState(d, who, hiddenKey, func(h *hidden) { h.Version = version })
}

// hidden is the version an admin hid.
type hidden struct {
	Version string `json:"version"`
}

// tried notes a failed try: the last answer with a new time, or an
// empty one.
func tried(path string, now time.Time) error {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(path, []byte("{}"), 0o600)
	}
	return os.Chtimes(path, now, now)
}

// cached reads the last answer; none without one.
func cached(dir string) (Release, bool) {
	raw, err := os.ReadFile(filepath.Join(dir, cacheName))
	if err != nil {
		return Release{}, false
	}
	var r Release
	if err := json.Unmarshal(raw, &r); err != nil || r.Version == "" {
		return Release{}, false
	}
	return r, true
}

// errNoVersion: a build without a release number ("dev").
var errNoVersion = errors.New("no version")

// Newer reports whether latest is above running, part by part as
// numbers: "0.10.0" > "0.9.3", missing parts count as 0, anything after
// "-", "+" or "/" is ignored ("0.7.0/main/#12" is 0.7.0). A dev build
// never gets a hint.
func Newer(latest, running string) bool {
	l, errL := parts(latest)
	r, errR := parts(running)
	if errL != nil || errR != nil {
		return false
	}
	for i := range max(len(l), len(r)) {
		a, b := at(l, i), at(r, i)
		if a != b {
			return a > b
		}
	}
	return false
}

// parts splits a version into its numbers, e.g. "v1.2-rc1" → [1 2].
func parts(v string) ([]int, error) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+/"); i >= 0 {
		v = v[:i]
	}
	if v == "" {
		return nil, errNoVersion
	}
	var out []int
	for _, p := range strings.Split(v, ".") {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil, errNoVersion
		}
		out = append(out, n)
	}
	return out, nil
}

// at is part i, 0 past the end.
func at(p []int, i int) int {
	if i < len(p) {
		return p[i]
	}
	return 0
}
