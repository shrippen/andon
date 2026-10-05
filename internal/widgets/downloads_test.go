package widgets

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/sources"
)

// downloadsFixture: two repos over three recorded days; b/two loses an
// asset on the last day, which counts as no downloads, not fewer.
func downloadsFixture() (map[string]any, ViewCtx) {
	day := func(back int) time.Time { return time.Date(2026, 9, 15-back, 0, 0, 0, 0, time.UTC) }
	h := &metrics.History{Series: map[string][]metrics.Point{
		metrics.DownloadsKey(enums.ServiceGitHub, "a/one"): {{Day: day(2), Value: 100}, {Day: day(1), Value: 130}, {Day: day(0), Value: 150}},
		metrics.DownloadsKey(enums.ServiceGitHub, "b/two"): {{Day: day(2), Value: 50}, {Day: day(1), Value: 55}, {Day: day(0), Value: 54}},
	}}
	data := &sources.GitHubDataset{Repos: []sources.GitRepo{
		{Name: "b/two", Downloads: 54, Release: "v1"}, {Name: "a/one", Downloads: 150, Release: "v3"}, {Name: "c/none"}}}
	return map[string]any{dataName: data, HistorySlot: h}, ViewCtx{Today: "2026-09-15"}
}

// TestDownloadsView: total of all repos, gains of the window, most
// gained first; a repo without downloads is left out.
func TestDownloadsView(t *testing.T) {
	results, ctx := downloadsFixture()
	v := downloadsView(downloadKinds["github_downloads"])(DownloadsConfig{Days: 7, Limit: 5}, results, ctx)
	repos := v["Repos"].([]ItemDownloads)
	if v["Total"] != 204.0 || v["Gain"] != 55.0 || len(repos) != 2 || repos[0].Name != "a/one" || repos[0].Gain != 50 || repos[1].Pct != 10 {
		t.Fatalf("view: %+v", v)
	}
	if bars := v["Bars"].([]LoadBar); len(bars) != 7 || bars[5].H != 100 || bars[6].H != 57 || bars[6].Title != "2026-09-15" {
		t.Fatalf("bars: %+v", bars)
	}
}

// TestDownloadsDetail: per-day graph and a row per repo with its gain.
func TestDownloadsDetail(t *testing.T) {
	results, ctx := downloadsFixture()
	body := downloadsDetail(downloadKinds["github_downloads"])(DownloadsConfig{Days: 7}, results, ctx).Body.(*DetailBody)
	if body.Facts[0].Value.(map[string]any)["$num"] != 204.0 || body.Facts[1].Value.(map[string]any)["$num"] != 55.0 || body.Blocks[0].Kind != BlockGraph {
		t.Fatalf("detail: %+v", body)
	}
	rows := body.Blocks[1].Data.(Table).Rows
	if len(rows) != 2 || rows[0][0].Href != "https://github.com/a/one/releases" || rows[1][2].Value.(map[string]any)["$num"] != 5.0 {
		t.Fatalf("rows: %+v", rows)
	}
}

// TestDownloadsCollecting: a single recorded day has no trend yet.
func TestDownloadsCollecting(t *testing.T) {
	data := &sources.GitHubDataset{Repos: []sources.GitRepo{{Name: "a/one", Downloads: 10, Release: "v1"}}}
	v := downloadsView(downloadKinds["github_downloads"])(DownloadsConfig{Days: 7}, map[string]any{dataName: data}, ViewCtx{Today: "2026-09-15"})
	if v["Bars"] != nil || v["Total"] != 10.0 {
		t.Fatalf("view: %+v", v)
	}
}

// TestDownloadsRead: the side names the oldest read and the interval.
func TestDownloadsRead(t *testing.T) {
	at := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	data := &sources.GitHubDataset{DownloadsEvery: 2 * time.Hour, DownloadsAuto: true,
		Repos: []sources.GitRepo{{DownloadsAt: at.Add(time.Hour)}, {DownloadsAt: at}, {}}}
	side := downloadsRead(data)
	every := side[1].Value.(map[string]any)
	if len(side) != 2 || side[0].Value.(map[string]any)["$ago"] != at.Format(time.RFC3339) || every["$t"] != "detail.downloads.every_auto" {
		t.Fatalf("side: %+v", side)
	}
	if downloadsRead(&sources.GitHubDataset{}) != nil {
		t.Fatal("demo without interval shows one")
	}
}
