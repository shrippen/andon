package sources

// GitHub release downloads change slowly and cost a call per 100
// releases, so they are read less often than the rest of the dataset
// and remembered in between:
//
//	fetch (every opsTTL) ─► memo[connection, repo] younger than every? ─yes─► remembered total
//	                                                                   └─no──► read releases, remember
//
//	option downloads_minutes   absent or "auto": hourly
//	                           a number: every that many minutes (at least opsTTL)
//
// The whole query is paced by GitHub's rate limit as every other one
// (svcdata.Pace): with many repos it runs less often.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"andon/internal/drivers/services"
)

const (
	githubPageSize = 100
	githubPages    = 10 // read at most 1000 releases or repos

	downloadsOption = "downloads_minutes"
	downloadsAuto   = time.Hour
)

// downloadMemo is a repo's last read total.
type downloadMemo struct {
	total int
	at    time.Time
}

var (
	downloadsMu   sync.Mutex
	downloadsSeen = map[string]downloadMemo{}
	clockNow      = time.Now // tests fix the clock
)

// downloadsEvery is the interval of the download reads and whether it
// is automatic.
func downloadsEvery(sctx Ctx) (time.Duration, bool) {
	if minutes := asFloat(sctx.Options[downloadsOption]); minutes > 0 {
		return max(time.Duration(minutes)*time.Minute, opsTTL), false
	}
	return downloadsAuto, true
}

// addDownloads fills in the downloads of the repos with a release, read
// again only once the remembered total is older than the interval. A
// failed read keeps the last total.
func addDownloads(ctx context.Context, api services.KeyedApi, sctx Ctx, data *GitHubDataset) {
	data.DownloadsEvery, data.DownloadsAuto = downloadsEvery(sctx)

	now := clockNow()
	for i, r := range data.Repos {
		if r.Release == "" {
			continue
		}

		key := memoKey(sctx, r.Name)
		downloadsMu.Lock()
		seen, ok := downloadsSeen[key]
		downloadsMu.Unlock()
		if !ok || now.Sub(seen.at) >= data.DownloadsEvery {
			if total, err := repoDownloads(ctx, api, "repos/"+r.Name); err == nil {
				seen = downloadMemo{total: total, at: now}
				downloadsMu.Lock()
				downloadsSeen[key] = seen
				downloadsMu.Unlock()
			}
		}
		data.Repos[i].Downloads, data.Repos[i].DownloadsAt = seen.total, seen.at
	}
}

// memoKey keeps connections apart: the same repo read with another
// token may show other figures. The token itself is not kept.
func memoKey(sctx Ctx, repo string) string {
	sum := sha256.Sum256([]byte(sctx.Secret))
	return sctx.URL + "\x00" + hex.EncodeToString(sum[:8]) + "\x00" + strings.ToLower(repo)
}

// repoDownloads adds up the downloads of every release asset; a failed
// page fails the whole sum, a partial one would look like a drop.
func repoDownloads(ctx context.Context, api services.KeyedApi, path string) (int, error) {
	total := 0
	for page := 1; page <= githubPages; page++ {
		raw, err := api.Get(ctx, path+"/releases", url.Values{"per_page": {strconv.Itoa(githubPageSize)}, "page": {strconv.Itoa(page)}})
		if err != nil {
			return 0, err
		}

		list := asList(raw)
		for _, rel := range list {
			for _, asset := range asList(asMap(rel)["assets"]) {
				total += int(asFloat(asMap(asset)["download_count"]))
			}
		}
		if len(list) < githubPageSize {
			break
		}
	}
	return total, nil
}
