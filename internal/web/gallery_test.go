package web_test

import (
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"andon/internal/enums"
	"andon/internal/i18n"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// Within each topic the gallery lists types A–Z by displayed name, and
// types without a connection preview demo data.
func TestGallerySortsByName(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	page := string(mustGet(t, srv, client, "/widgets/new?dialog"))
	sorter := collate.New(language.German, collate.IgnoreCase)
	sections := regexp.MustCompile(`(?s)<section id="topic-(\w+)".*?</section>`).FindAllStringSubmatch(page, -1)
	if len(sections) < 5 {
		t.Fatalf("expected topic sections, got %d", len(sections))
	}
	for _, sec := range sections {
		var names []string
		for _, m := range regexp.MustCompile(`<b>([^<]+)</b>`).FindAllStringSubmatch(sec[0], -1) {
			names = append(names, m[1])
		}
		if !slices.IsSortedFunc(names, sorter.CompareString) {
			t.Errorf("%s not sorted: %v", sec[1], names)
		}
	}
	if !strings.Contains(page, `href="/connections/new?service=kimai"`) {
		t.Fatalf("expected a connect link for Kimai without a connection")
	}

	space := regexp.MustCompile(`space=(\d+)`).FindStringSubmatch(page)[1]
	sample := string(mustGet(t, srv, client, "/widget-sample/kimai_week?space="+space))
	if !strings.Contains(sample, "weekcol") {
		t.Fatalf("expected demo week columns:\n%s", sample)
	}
}

// From the gallery a set-up tile is placed as a copy, and a new tile can
// be two rows high and two columns wide.
func TestGalleryCopyAndRows(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	space := regexp.MustCompile(`space=(\d+)`).FindSubmatch(mustGet(t, srv, client, "/widgets/new?dialog"))[1]
	resp, err := client.PostForm(srv.URL+"/widgets", url.Values{
		"csrf": {csrfToken(t, srv, client)}, "space_id": {string(space)}, "type": {"note"}, "title": {"My Note"}, "cfg.text": {"hi"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	resp.Body.Close()

	boardURL, section, version, widget := placeTarget(t, srv, client, "My Note")
	board := boardIDFrom(boardURL)
	target := url.Values{"space_id": {string(space)}, "section_id": {section}, "board_id": {board}, "version": {version}}

	form := url.Values{"csrf": {csrfToken(t, srv, client)}}
	for k, v := range target {
		form[k] = v
	}
	resp, err = client.PostForm(srv.URL+"/widgets/"+widget+"/copy", form)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "/edit?board_id="+board) {
		t.Fatalf("copy: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if body := string(mustGet(t, srv, client, boardURL+"?edit")); !strings.Contains(body, "My Note") {
		t.Fatalf("expected the copy on the board:\n%s", body)
	}

	// The copy bumped the board version once.
	form = url.Values{"csrf": {csrfToken(t, srv, client)}, "type": {"note"}, "title": {"Tall"}, "cfg.text": {"x"}, "rows": {"2"}, "cols": {"2"}}
	for k, v := range target {
		form[k] = v
	}
	form.Set("version", nextVersion(t, version))
	resp, err = client.PostForm(srv.URL+"/widgets", form)
	if err != nil {
		t.Fatalf("create tall: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("create tall: %d", resp.StatusCode)
	}
	if body := string(mustGet(t, srv, client, boardURL+"?edit")); !strings.Contains(body, `data-rows="2"`) {
		t.Fatalf("expected a two-row tile:\n%s", body)
	}
	if body := string(mustGet(t, srv, client, boardURL+"?edit")); !strings.Contains(body, `data-cols="2"`) {
		t.Fatalf("expected a two-column tile:\n%s", body)
	}
}

func nextVersion(t *testing.T, v string) string {
	t.Helper()
	n, err := strconv.Atoi(v)
	if err != nil {
		t.Fatalf("version %q: %v", v, err)
	}
	return strconv.Itoa(n + 1)
}

// A tile arrives with the page, so the board doesn't grow as fragments
// load; a note has nothing to refresh and asks for no fragment on load.
func TestBoardRendersTilesWithPage(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	space := regexp.MustCompile(`space=(\d+)`).FindSubmatch(mustGet(t, srv, client, "/widgets/new?dialog"))[1]
	resp, err := client.PostForm(srv.URL+"/widgets", url.Values{
		"csrf": {csrfToken(t, srv, client)}, "space_id": {string(space)}, "type": {"note"}, "title": {"My Note"}, "cfg.text": {"Inline body"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	resp.Body.Close()
	boardURL, section, version, widget := placeTarget(t, srv, client, "My Note")
	resp, err = client.PostForm(srv.URL+"/boards/"+boardIDFrom(boardURL)+"/sections/"+section+"/place", url.Values{
		"csrf": {csrfToken(t, srv, client)}, "widget_id": {widget}, "version": {version},
	})
	if err != nil {
		t.Fatalf("place: %v", err)
	}
	resp.Body.Close()

	page := string(mustGet(t, srv, client, boardURL))
	tile := regexp.MustCompile(`(?s)<div class="tile-slot w-note".*?</article>`).FindString(page)
	if !strings.Contains(tile, "Inline body") || strings.Contains(tile, `hx-trigger="load`) {
		t.Fatalf("expected the note rendered inline without a fragment request:\n%s", tile)
	}
}

// TestFieldLabelPerType: payment_days' target is days, not the link
// tile's "open in".
func TestFieldLabelPerType(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	space := regexp.MustCompile(`space=(\d+)`).FindSubmatch(mustGet(t, srv, client, "/widgets/new?dialog"))[1]
	body := string(mustGet(t, srv, client, "/widgets/new?dialog&type=payment_days&space="+string(space)))
	label := regexp.MustCompile(`<label for="cfg.target_days">([^<]*)</label>`).FindStringSubmatch(body)
	if label == nil {
		t.Fatalf("no target field:\n%s", body)
	}
	for _, loc := range []enums.Locale{enums.LocaleDE, enums.LocaleEN} {
		if label[1] == i18n.T("field.target", loc, nil) {
			t.Fatalf("target labelled as the link's %q", label[1])
		}
	}
}

// TestWidgetBadWindowRefused: a maintenance window like "25-3" is not
// saved; the form says why.
func TestWidgetBadWindowRefused(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	space := regexp.MustCompile(`space=(\d+)`).FindSubmatch(mustGet(t, srv, client, "/widgets/new?dialog"))[1]
	resp, err := client.PostForm(srv.URL+"/widgets", url.Values{
		"csrf": {csrfToken(t, srv, client)}, "space_id": {string(space)}, "type": {"update_window"}, "cfg.window": {"25-3"},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(string(body), i18n.T("widget.bad_window", enums.LocaleDE, nil)) &&
		!strings.Contains(string(body), i18n.T("widget.bad_window", enums.LocaleEN, nil)) {
		t.Fatalf("status %d:\n%s", resp.StatusCode, body)
	}
}

// TestNumberFieldHasRange: the form tells the browser a field's range.
func TestNumberFieldHasRange(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	space := regexp.MustCompile(`space=(\d+)`).FindSubmatch(mustGet(t, srv, client, "/widgets/new?dialog"))[1]
	body := string(mustGet(t, srv, client, "/widgets/new?dialog&type=hints&space="+string(space)))
	if !strings.Contains(body, `name="cfg.limit" type="number" step="any" value="8" min="1" max="50"`) {
		t.Fatalf("no range on limit:\n%s", body)
	}
}

// TestGalleryOneReuseDialog: set-up tiles share one reuse dialog, filled
// when a card's button opens it; a dialog with three forms per tile made
// the gallery 580 KB and 8,000 nodes with 226 tiles.
func TestGalleryOneReuseDialog(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	space := regexp.MustCompile(`space=(\d+)`).FindSubmatch(mustGet(t, srv, client, "/widgets/new?dialog"))[1]
	for _, title := range []string{"Note A", "Note B"} {
		resp, err := client.PostForm(srv.URL+"/widgets", url.Values{
			"csrf": {csrfToken(t, srv, client)}, "space_id": {string(space)}, "type": {"note"}, "title": {title}, "cfg.text": {"hi"},
		})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	resp := getFollowingRedirect(t, srv, client, "/")
	resp.Body.Close()
	add := addLinkRe.Find(mustGet(t, srv, client, resp.Request.URL.Path+"?edit"))
	page := string(mustGet(t, srv, client, strings.ReplaceAll(string(add), "&amp;", "&")+"&dialog"))
	if n := strings.Count(page, `class="dialog gal-dialog"`); n != 1 {
		t.Fatalf("expected one reuse dialog, got %d", n)
	}
	if n := len(regexp.MustCompile(`data-open="reuse" data-reuse="\d+"`).FindAllString(page, -1)); n != 2 {
		t.Fatalf("expected 2 cards opening the reuse dialog, got %d", n)
	}
	if !strings.Contains(page, `action="/widgets/{widget}/copy"`) {
		t.Fatal("reuse dialog lacks the copy form")
	}
}

// TestLibraryOneRowMenu: the library's row menus come from one template,
// filled when a menu opens; a copy form with a space picker per row cost
// Chrome 280 ms on a 226-tile library.
func TestLibraryOneRowMenu(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	space := regexp.MustCompile(`space=(\d+)`).FindSubmatch(mustGet(t, srv, client, "/widgets/new?dialog"))[1]
	for _, title := range []string{"Note A", "Note B"} {
		resp, err := client.PostForm(srv.URL+"/widgets", url.Values{
			"csrf": {csrfToken(t, srv, client)}, "space_id": {string(space)}, "type": {"note"}, "title": {title}, "cfg.text": {"hi"},
		})
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}

	page := string(mustGet(t, srv, client, "/widgets"))
	if n := strings.Count(page, `name="space_id"`); n != 1 {
		t.Fatalf("expected one space picker, got %d", n)
	}
	if n := len(regexp.MustCompile(`<details class="row-more" data-widget="\d+">`).FindAllString(page, -1)); n != 2 {
		t.Fatalf("expected 2 row menus to fill, got %d", n)
	}
	if !strings.Contains(page, `<template id="row-more">`) || !strings.Contains(page, `action="/widgets/{widget}/copy"`) {
		t.Fatal("row menu template missing")
	}
}
