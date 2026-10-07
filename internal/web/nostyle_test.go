//go:build !release

package web_test

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"testing"

	"andon/internal/services/seed"
)

// inlineStyle is a style attribute in markup; the CSP refuses it
// (style-src without 'unsafe-inline'). data-style does not match.
var inlineStyle = regexp.MustCompile(`<[a-zA-Z][^>]*\sstyle\s*=`)

// pageLink finds the same-origin targets a page reaches: links, htmx
// fragments, tile dialogs and hint pop-ups.
var pageLink = regexp.MustCompile(`(?:href|hx-get|data-details|data-hints)="(/[^"]*)"`)

// skipCrawl are targets that are no HTML page of the app or end the
// session: downloads, icons, the e-mail preview (mail clients need
// inline styles; its own CSP allows them) and logout. Gallery samples
// fetch the internet; TestEveryTileRendersFromDemo covers their tiles.
var skipCrawl = regexp.MustCompile(`^/(logout|widget-sample/|icons/|static/|theme|theme-fonts/|me/notify/digest/preview|calendar\.ics|api/|embed/|auth/)|export|\.csv|\.ics|/file\b|/thumb/|/reapply|/code$|kiosk`)

// perShape is how many targets of one shape the walk opens: /boards/3
// and /boards/7 render the same template, ?source=a and ?source=b too.
const perShape = 3

var digits = regexp.MustCompile(`[0-9]+`)

// shapeOf is a target without its numbers and query values:
// "/clients/2/14?kimai=3" → "/clients/#/#?kimai".
func shapeOf(target string) string {
	path, query, _ := strings.Cut(target, "?")
	shape := digits.ReplaceAllString(path, "#")
	if values, err := url.ParseQuery(query); err == nil && len(values) > 0 {
		keys := make([]string, 0, len(values))
		for k := range values {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		shape += "?" + strings.Join(keys, "&")
	}
	return shape
}

// TestNoInlineStyles walks every page, fragment and dialog the demo world
// reaches as admin and fails on any style attribute.
func TestNoInlineStyles(t *testing.T) {
	if testing.Short() {
		t.Skip("crawls the demo world")
	}
	srv, client, _ := newTestServer(t)
	database, _ := testDBs.Load(srv.URL)
	if err := seed.Demo(context.Background(), database.(*sql.DB)); err != nil {
		t.Fatal(err)
	}
	resp := postForm(t, client, srv.URL+"/login", url.Values{"email": {seed.DemoAdmin}, "password": {seed.DemoPassword}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("demo login: %d", resp.StatusCode)
	}

	seen := map[string]bool{"/": true}
	shapes := map[string]int{}
	queue := []string{"/"}
	for len(queue) > 0 {
		target := queue[0]
		queue = queue[1:]
		resp, err := client.Get(srv.URL + target)
		if err != nil {
			t.Fatalf("get %s: %v", target, err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/html") {
			continue
		}
		page := string(body)
		if m := inlineStyle.FindString(page); m != "" {
			t.Errorf("%s: inline style in %s", target, m)
		}

		for _, link := range pageLink.FindAllStringSubmatch(page, -1) {
			next := strings.ReplaceAll(strings.SplitN(link[1], "#", 2)[0], "&amp;", "&")
			if next == "" || seen[next] || skipCrawl.MatchString(next) || shapes[shapeOf(next)] >= perShape {
				continue
			}
			seen[next] = true
			shapes[shapeOf(next)]++
			queue = append(queue, next)
		}
	}
	if len(seen) < 100 {
		t.Fatalf("walk reached only %d pages", len(seen))
	}
	t.Logf("checked %d pages and fragments of %d kinds", len(seen), len(shapes))
}
