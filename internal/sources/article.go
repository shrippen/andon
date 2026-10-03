package sources

// The full text of a feed's newest entry, read when the RSS dialog opens
// and kept only in the cache: the page's paragraphs from its densest
// block, without navigation, scripts or forms.
//
//	<body><nav>…</nav><article><h1>T</h1><p>A</p><p>B</p></article></body>
//	  → Article{Paragraphs: ["A", "B"]}

import (
	"context"
	"io"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"andon/internal/drivers/httpclient"
)

const (
	articleMax       = 2 << 20 // bytes of HTML read
	articleParagraph = 40      // shorter text blocks are captions, bylines, buttons
	articleMaxParas  = 60
)

// Article is the readable text of a page.
type Article struct {
	Link       string
	Paragraphs []string
}

var ArticleSource = source{key: "rss.article", ttl: feedTTL, fetch: fetchArticle}

// fetchArticle reads the tile's feeds (same params as "rss") and the page
// of their newest entry.
func fetchArticle(ctx context.Context, sctx Ctx) (any, error) {
	raw, err := fetchFeedSource(ctx, sctx)
	if err != nil {
		return nil, err
	}
	feed, ok := raw.(*FeedResult)
	if !ok || len(feed.Items) == 0 || feed.Items[0].Link == "" {
		return &Article{}, nil
	}
	link := feed.Items[0].Link
	resp, err := httpclient.Request(ctx, "GET", link, httpclient.Options{})
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}
	defer resp.Body.Close()
	return &Article{Link: link, Paragraphs: readable(io.LimitReader(resp.Body, articleMax))}, nil
}

// skipped are elements whose text is never part of the article.
var skipped = map[atom.Atom]bool{atom.Script: true, atom.Style: true, atom.Nav: true, atom.Header: true,
	atom.Footer: true, atom.Aside: true, atom.Form: true, atom.Noscript: true, atom.Figure: true}

// readable picks the element whose direct <p> children hold the most
// text and returns those paragraphs.
func readable(r io.Reader) []string {
	doc, err := html.Parse(r)
	if err != nil {
		return nil
	}
	var best []string
	bestLen := 0
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && skipped[n.DataAtom] {
			return
		}
		var paras []string
		size := 0
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type != html.ElementNode || c.DataAtom != atom.P {
				continue
			}
			text := strings.Join(strings.Fields(textOf(c)), " ")
			if len(text) >= articleParagraph {
				paras = append(paras, text)
				size += len(text)
			}
		}
		if size > bestLen {
			best, bestLen = paras, size
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return best[:min(len(best), articleMaxParas)]
}

// textOf is a node's text without skipped elements.
func textOf(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	if n.Type == html.ElementNode && skipped[n.DataAtom] {
		return ""
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(textOf(c))
	}
	return b.String()
}

func init() { Register(ArticleSource) }
