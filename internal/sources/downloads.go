package sources

// Downloads of published things, whatever the service: a GitHub repo's
// release assets, a KDE Store entry. Services only keep a running total;
// the history records it daily and the downloads tile shows the gains.
//
//	GitHubDataset.Downloads()   ┐
//	KDEStoreDataset.Downloads() ┴─► []DownloadItem ─► recorder, tile

import "time"

// DownloadItem is one thing with a download counter.
type DownloadItem struct {
	ID       string // stable within the service: "studio/website", "2368175"
	Name     string
	URL      string // its page
	Version  string
	Released time.Time
	Total    int           // 0 = unknown
	Days     []DownloadDay // earlier day totals, oldest first (demo only)
}

// DownloadDay is an item's download total at the end of a day.
type DownloadDay struct {
	Day   string // 2006-01-02
	Total int
}

// Downloadable is a dataset that lists download counters.
type Downloadable interface {
	Downloads() []DownloadItem
}

// githubWeb precedes "owner/name" on GitHub's site.
const githubWeb = "https://github.com/"

// Downloads lists the repos with a release.
func (d *GitHubDataset) Downloads() []DownloadItem {
	var out []DownloadItem
	for _, r := range d.Repos {
		if r.Release == "" {
			continue
		}
		out = append(out, DownloadItem{ID: r.Name, Name: r.Name, URL: githubWeb + r.Name + "/releases", Version: r.Release,
			Released: r.ReleasedAt, Total: r.Downloads, Days: r.DownloadDays})
	}
	return out
}

// daysBack turns a demo's downloads per day (today last) into the day
// totals before today, counted back from the current total:
//
//	total 100, daily [5, 7, 3] → 2 days ago 90, yesterday 97
func daysBack(total int, daily []int, now time.Time) []DownloadDay {
	var out []DownloadDay
	for back, n := 0, len(daily); back < n-1; back++ {
		total -= daily[n-1-back]
		out = append([]DownloadDay{{Day: now.AddDate(0, 0, -(back + 1)).Format(time.DateOnly), Total: total}}, out...)
	}
	return out
}
