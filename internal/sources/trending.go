package sources

// GitHub trending: the repositories created in the last day, week or
// month with the most stars, optionally of one language. GitHub has no
// API for its trending page; the search API with a creation date is the
// official stand-in. No token (10 searches a minute are plenty for an
// hourly tile).
//
//	GET api.github.com/search/repositories?q=created:>=2026-09-29 language:go&sort=stars&order=desc

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/httpclient"
)

const (
	trendingTTL = time.Hour

	// maxTrending caps the list; the search API hands out up to 100.
	maxTrending = 30
)

// Periods a trending list looks back.
const (
	TrendDaily   = "daily"
	TrendWeekly  = "weekly"
	TrendMonthly = "monthly"
)

// trendDays is how far back each period looks.
var trendDays = map[string]int{TrendDaily: 1, TrendWeekly: 7, TrendMonthly: 30}

var githubSearchBase = "https://api.github.com"

// TrendRepo is one repository of the list.
type TrendRepo struct {
	Name, URL, Description, Language string
	Stars                            int
}

// TrendingResult lists repositories by stars, most first.
type TrendingResult struct{ Repos []TrendRepo }

var TrendingSource = source{key: "github.trending", ttl: trendingTTL, fetch: fetchTrending}

func fetchTrending(ctx context.Context, sctx Ctx) (any, error) {
	days, ok := trendDays[asStr(sctx.Params["since"])]
	if !ok {
		days = trendDays[TrendWeekly]
	}
	limit := min(max(int(asFloat(sctx.Params["limit"])), 1), maxTrending)

	// e.g. "created:>=2026-09-29 language:go"
	q := "created:>=" + time.Now().UTC().AddDate(0, 0, -days).Format(isoDay)
	if lang := strings.TrimSpace(asStr(sctx.Params["language"])); lang != "" {
		q += " language:" + lang
	}
	query := url.Values{"q": {q}, "sort": {"stars"}, "order": {"desc"}, "per_page": {strconv.Itoa(limit)}}
	body, _, err := httpclient.GetJSON(ctx, githubSearchBase+"/search/repositories", httpclient.Options{
		Params: query, Headers: map[string]string{"Accept": "application/vnd.github+json"},
	})
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}

	out := &TrendingResult{}
	for _, raw := range asList(asMap(body)["items"]) {
		m := asMap(raw)
		out.Repos = append(out.Repos, TrendRepo{
			Name: asStr(m["full_name"]), URL: asStr(m["html_url"]), Description: asStr(m["description"]),
			Language: asStr(m["language"]), Stars: int(asFloat(m["stargazers_count"])),
		})
	}
	return out, nil
}

func init() {
	Register(TrendingSource)
}
