package sources

// Reading: the front pages of Hacker News, Lobsters, subreddits and the
// newest videos of YouTube channels, each item with points, comments and
// age. No login. The option sites says what is read, separated by commas:
//
//	hackernews, lobsters, r/selfhosted, youtube:UCxxxxxxxxxxxxxxxxxxxxxx
//
//	GET hn.algolia.com/api/v1/search?tags=front_page      → {hits: [{objectID, title, url, points, num_comments, created_at_i}]}
//	GET lobste.rs/hottest.json                             → [{title, url, score, comment_count, created_at, comments_url}]
//	GET www.reddit.com/r/<sub>/hot.json?limit=N            → {data: {children: [{data: {title, url, score, num_comments, created_utc, permalink, stickied}}]}}
//	GET www.youtube.com/feeds/videos.xml?channel_id=<id>   → Atom, views in media:statistics

import (
	"context"
	"encoding/xml"
	"net/url"
	"strconv"
	"strings"
	"time"

	"andon/internal/drivers/httpclient"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

// Sites the reading connection knows.
const (
	SiteHackerNews = "hackernews"
	SiteLobsters   = "lobsters"
	SiteReddit     = "reddit"
	SiteYouTube    = "youtube"
)

const (
	newsTTL     = 30 * time.Minute
	newsPerSite = 15

	redditPrefix  = "r/"
	youtubePrefix = "youtube:"
	hnItem        = "https://news.ycombinator.com/item?id="
)

// newsDefault is read when the option sites is empty.
var newsDefault = []string{SiteHackerNews, SiteLobsters}

var (
	hnBase       = "https://hn.algolia.com"
	lobstersBase = "https://lobste.rs"
	redditBase   = "https://www.reddit.com"
	youtubeBase  = "https://www.youtube.com"
)

// NewsItem is one post or video. URL is what it links, Link its
// discussion (the video itself on YouTube).
type NewsItem struct {
	Site     string
	Feed     string // subreddit or channel, "" on Hacker News and Lobsters
	Title    string
	URL      string
	Link     string
	Points   int
	Comments int
	Views    int // YouTube only
	At       time.Time
}

// NewsDataset is every site's items in the site's own order, and the
// sites that did not answer.
type NewsDataset struct {
	URL    string
	Items  []NewsItem
	Failed []string
}

var NewsData = source{key: "news.data", ttl: newsTTL, service: enums.ServiceNews, fetch: fetchNews}

func fetchNews(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoNews(time.Now().UTC()), nil
	}
	data := &NewsDataset{URL: sctx.URL}
	sites := newsSites(sctx.Options["sites"])
	for _, site := range sites {
		items, err := readSite(ctx, site)
		if err != nil {
			data.Failed = append(data.Failed, site)
			continue
		}
		data.Items = append(data.Items, items...)
	}
	if len(data.Failed) == len(sites) {
		return nil, newSourceError("reading.none_answered")
	}
	return data, nil
}

// newsSites reads the option sites; empty means newsDefault.
func newsSites(raw any) []string {
	if out := optionList(raw); len(out) > 0 {
		return out
	}
	return newsDefault
}

// optionList reads a list option: "a, b" or ["a", "b"].
func optionList(raw any) []string {
	var parts []string
	if list := asList(raw); len(list) > 0 {
		for _, v := range list {
			parts = append(parts, asStr(v))
		}
	} else {
		parts = strings.Split(asStr(raw), ",")
	}

	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func readSite(ctx context.Context, site string) ([]NewsItem, error) {
	lower := strings.ToLower(site)
	switch {
	case lower == SiteHackerNews:
		return readHackerNews(ctx)
	case lower == SiteLobsters:
		return readLobsters(ctx)
	case strings.HasPrefix(lower, redditPrefix):
		return readReddit(ctx, strings.TrimSpace(site[len(redditPrefix):]))
	case strings.HasPrefix(lower, youtubePrefix):
		return readYouTube(ctx, strings.TrimSpace(site[len(youtubePrefix):]))
	}
	return nil, newSourceError("reading.unknown_site")
}

func readHackerNews(ctx context.Context) ([]NewsItem, error) {
	body, _, err := httpclient.GetJSON(ctx, hnBase+"/api/v1/search", httpclient.Options{
		Params: url.Values{"tags": {"front_page"}, "hitsPerPage": {strconv.Itoa(newsPerSite)}}})
	if err != nil {
		return nil, err
	}
	var out []NewsItem
	for _, raw := range asList(asMap(body)["hits"]) {
		m := asMap(raw)
		link := hnItem + asStr(m["objectID"])
		out = append(out, NewsItem{Site: SiteHackerNews, Title: asStr(m["title"]), URL: firstStr(asStr(m["url"]), link), Link: link,
			Points: int(asFloat(m["points"])), Comments: int(asFloat(m["num_comments"])), At: time.Unix(asInt64(m["created_at_i"]), 0).UTC()})
	}
	return out, nil
}

func readLobsters(ctx context.Context) ([]NewsItem, error) {
	body, _, err := httpclient.GetJSON(ctx, lobstersBase+"/hottest.json", httpclient.Options{})
	if err != nil {
		return nil, err
	}
	var out []NewsItem
	for _, raw := range upTo(asList(body), newsPerSite) {
		m := asMap(raw)
		link := asStr(m["comments_url"])
		out = append(out, NewsItem{Site: SiteLobsters, Title: asStr(m["title"]), URL: firstStr(asStr(m["url"]), link), Link: link,
			Points: int(asFloat(m["score"])), Comments: int(asFloat(m["comment_count"])), At: parseTime(m["created_at"])})
	}
	return out, nil
}

func readReddit(ctx context.Context, sub string) ([]NewsItem, error) {
	body, _, err := httpclient.GetJSON(ctx, redditBase+"/r/"+url.PathEscape(sub)+"/hot.json", httpclient.Options{
		Params: url.Values{"limit": {strconv.Itoa(newsPerSite)}}})
	if err != nil {
		return nil, err
	}
	var out []NewsItem
	for _, raw := range asList(asMap(asMap(body)["data"])["children"]) {
		m := asMap(asMap(raw)["data"])
		if asBool(m["stickied"]) {
			continue // the moderators' pinned posts, the same every day
		}
		link := redditBase + asStr(m["permalink"])
		out = append(out, NewsItem{Site: SiteReddit, Feed: sub, Title: asStr(m["title"]), URL: firstStr(asStr(m["url"]), link), Link: link,
			Points: int(asFloat(m["score"])), Comments: int(asFloat(m["num_comments"])), At: time.Unix(int64(asFloat(m["created_utc"])), 0).UTC()})
	}
	return out, nil
}

// youtubeFeed is a channel's Atom feed; media:group carries the views.
type youtubeFeed struct {
	Title   string `xml:"title"`
	Entries []struct {
		Title string `xml:"title"`
		Link  struct {
			Href string `xml:"href,attr"`
		} `xml:"link"`
		Published string `xml:"published"`
		Group     struct {
			Community struct {
				Statistics struct {
					Views int `xml:"views,attr"`
				} `xml:"statistics"`
				StarRating struct {
					Count int `xml:"count,attr"`
				} `xml:"starRating"`
			} `xml:"community"`
		} `xml:"group"`
	} `xml:"entry"`
}

func readYouTube(ctx context.Context, channel string) ([]NewsItem, error) {
	text, err := httpclient.GetText(ctx, youtubeBase+"/feeds/videos.xml", httpclient.Options{Params: url.Values{"channel_id": {channel}}})
	if err != nil {
		return nil, err
	}
	var feed youtubeFeed
	if err := xml.Unmarshal([]byte(text), &feed); err != nil {
		return nil, err
	}
	var out []NewsItem
	for _, e := range upTo(feed.Entries, newsPerSite) {
		c := e.Group.Community
		out = append(out, NewsItem{Site: SiteYouTube, Feed: feed.Title, Title: e.Title, URL: e.Link.Href, Link: e.Link.Href,
			Points: c.StarRating.Count, Views: c.Statistics.Views, At: parseTime(e.Published)})
	}
	return out, nil
}

// upTo is the first n of list.
func upTo[T any](list []T, n int) []T {
	return list[:min(len(list), n)]
}

func DemoNews(now time.Time) *NewsDataset {
	data := &NewsDataset{}
	demoworld.MustDecode("news", now, data)
	return data
}

func init() {
	Register(NewsData)
	Register(testOf{NewsData, func(d any) map[string]any { return map[string]any{"items": len(d.(*NewsDataset).Items)} }})
}
