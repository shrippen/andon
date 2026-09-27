package sources

import (
	"encoding/xml"
	"errors"
	"html"
	"io"
	"regexp"
	"strings"
	"time"
)

const (
	titleMaxLen   = 200
	summaryMaxLen = 240
)

// mediaRef is an element pointing at a picture by attribute: enclosure,
// media:thumbnail, media:content.
type mediaRef struct {
	URL  string `xml:"url,attr"`
	Type string `xml:"type,attr"`
}

type rssDoc struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Title string `xml:"title"`
		Items []struct {
			Title       string     `xml:"title"`
			Link        string     `xml:"link"`
			PubDate     string     `xml:"pubDate"`
			Description string     `xml:"description"`
			Encoded     string     `xml:"encoded"`
			Enclosure   []mediaRef `xml:"enclosure"`
			Thumbnail   []mediaRef `xml:"thumbnail"`
			Content     []mediaRef `xml:"content"`
		} `xml:"item"`
	} `xml:"channel"`
}

type atomDoc struct {
	XMLName xml.Name `xml:"feed"`
	Title   string   `xml:"title"`
	Entries []struct {
		Title string `xml:"title"`
		Link  struct {
			Href string `xml:"href,attr"`
		} `xml:"link"`
		Updated   string     `xml:"updated"`
		Published string     `xml:"published"`
		Summary   string     `xml:"summary"`
		Content   string     `xml:"content"`
		Thumbnail []mediaRef `xml:"thumbnail"`
	} `xml:"entry"`
}

var imgSrc = regexp.MustCompile(`(?i)<img[^>]+src=["']([^"']+)["']`)

// feedImage is an item's picture: an image enclosure, a media thumbnail
// or content, else the first <img> in its HTML; http(s) only.
func feedImage(html []string, refs ...[]mediaRef) string {
	for _, list := range refs {
		for _, r := range list {
			if r.URL != "" && (r.Type == "" || strings.HasPrefix(r.Type, "image/")) {
				if link := httpLinkOnly(r.URL); link != "" {
					return link
				}
			}
		}
	}
	for _, h := range html {
		if m := imgSrc.FindStringSubmatch(h); m != nil {
			return httpLinkOnly(m[1])
		}
	}
	return ""
}

var errInvalidFeed = errors.New("invalid feed")

// parseFeed reads an RSS 2.0 or Atom feed body. A deliberately small
// parser (no third-party feedparser equivalent in Go): common fields
// only, not every extension format in the wild.
func parseFeed(r io.Reader) (*FeedResult, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	var rss rssDoc
	if xml.Unmarshal(raw, &rss) == nil {
		out := &FeedResult{Title: plainText(rss.Channel.Title, titleMaxLen)}
		for _, item := range rss.Channel.Items {
			out.Items = append(out.Items, FeedItem{
				Title: plainText(item.Title, titleMaxLen), Link: httpLinkOnly(item.Link),
				Published: parseFeedDate(item.PubDate), Summary: plainText(item.Description, summaryMaxLen),
				imageURL: feedImage([]string{item.Description, item.Encoded}, item.Enclosure, item.Thumbnail, item.Content),
			})
		}
		return out, nil
	}

	var atom atomDoc
	if xml.Unmarshal(raw, &atom) == nil {
		out := &FeedResult{Title: plainText(atom.Title, titleMaxLen)}
		for _, entry := range atom.Entries {
			published := entry.Published
			if published == "" {
				published = entry.Updated
			}
			out.Items = append(out.Items, FeedItem{
				Title: plainText(entry.Title, titleMaxLen), Link: httpLinkOnly(entry.Link.Href),
				Published: parseFeedDate(published), Summary: plainText(entry.Summary, summaryMaxLen),
				imageURL: feedImage([]string{entry.Summary, entry.Content}, entry.Thumbnail),
			})
		}
		return out, nil
	}

	return nil, errInvalidFeed
}

func httpLinkOnly(link string) string {
	if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") {
		return link
	}
	return ""
}

var feedDateLayouts = []string{time.RFC1123Z, time.RFC1123, time.RFC3339}

func parseFeedDate(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	for _, layout := range feedDateLayouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return ""
}

var tagPattern = regexp.MustCompile(`<[^>]*>`)

// plainText strips any HTML markup (feeds are third-party content — no
// markup reaches the page) and collapses whitespace, truncating to max
// runes with an ellipsis.
func plainText(raw string, max int) string {
	text := html.UnescapeString(tagPattern.ReplaceAllString(raw, " "))
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= max {
		return text
	}
	return string(runes[:max]) + "…"
}
