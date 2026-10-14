package web_test

import (
	"net/url"
	"regexp"
	"strings"
	"testing"
)

// A Verbund's categories page shows the EÜR field it found; the EÜR
// bench asks for it until saved, then lists expenses to fill.
func TestEuerPages(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	var conns []string
	for _, service := range []string{"invoiceninja", "sure"} {
		resp := postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
			"service": {service}, "name": {service}, "url": {"demo://" + service}, "mode": {"shared"}, "tls": {"verify"}})
		conns = append(conns, regexp.MustCompile(`/connections/(\d+)`).FindStringSubmatch(resp.Header.Get("Location"))[1])
	}
	postForm(t, client, srv.URL+"/verbund", url.Values{"csrf": {csrfToken(t, srv, client)}, "name": {"Studio"}, "conn": conns})
	list := string(mustGet(t, srv, client, "/spaces/"+space+"/verbund"))
	m := regexp.MustCompile(`href="/verbund/(\d+)/categories\?space=`).FindStringSubmatch(list)
	if m == nil {
		t.Fatalf("no categories link:\n%s", list)
	}

	page := string(mustGet(t, srv, client, "/euer"))
	if !strings.Contains(page, "Welches Feld hält die EÜR-Zeile?") || !strings.Contains(page, "/verbund/"+m[1]+"/categories") {
		t.Fatalf("euer before setup:\n%s", page)
	}
	cats := string(mustGet(t, srv, client, "/verbund/"+m[1]+"/categories?space="+space))
	if !strings.Contains(cats, `<option value="3" selected>EÜR</option>`) || !strings.Contains(cats, `name="euer:`) || !strings.Contains(cats, `name="sure:`) {
		t.Fatalf("categories page:\n%s", cats)
	}

	form := url.Values{"csrf": {csrfToken(t, srv, client)}, "space": {space}, "euer_slot": {"3"}}
	for _, k := range regexp.MustCompile(`name="(euer:[^"]+)" list="euer-lines" value="([^"]*)"`).FindAllStringSubmatch(cats, -1) {
		form.Set(k[1], k[2])
	}
	resp := postForm(t, client, srv.URL+"/verbund/"+m[1]+"/categories", form)
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "verbund.categories_saved") {
		t.Fatalf("save: %s", loc)
	}
	page = string(mustGet(t, srv, client, "/euer"))
	if !strings.Contains(page, `name="act_`) || !strings.Contains(page, "davon mit Vorschlag") {
		t.Fatalf("euer after setup:\n%s", page)
	}
}
