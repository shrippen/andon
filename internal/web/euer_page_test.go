package web_test

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
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

// The vendors page opens for an Invoice Ninja and reviews a merge.
func TestVendorsPage(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
		"service": {"invoiceninja"}, "name": {"Ninja"}, "url": {"demo://invoiceninja"}, "mode": {"shared"}, "tls": {"verify"}})
	if page := string(mustGet(t, srv, client, "/vendors")); !strings.Contains(page, "Lieferanten aufräumen") {
		t.Fatalf("vendors page:\n%s", page)
	}
	resp, err := client.PostForm(srv.URL+"/vendors/review", url.Values{"csrf": {csrfToken(t, srv, client)}, "pick": {"demo-v2"},
		"act_demo-v2": {"merge"}, "into_demo-v2": {"demo-v1"}})
	if err != nil {
		t.Fatal(err)
	}
	if review := readBody(t, resp); !strings.Contains(review, "Zusammenführen bestätigen") || !strings.Contains(review, `name="into_demo-v2" value="demo-v1"`) {
		t.Fatalf("review:\n%s", review)
	}
}

// "Close the week" opens on a demo Kimai and says what it reads from.
func TestWeekPage(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	for _, service := range []string{"kimai", "dawarich"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
			"service": {service}, "name": {service}, "url": {"demo://" + service}, "mode": {"shared"}, "tls": {"verify"}})
	}
	page := string(mustGet(t, srv, client, "/week"))
	for _, want := range []string{"Woche schließen", "Gelesen aus:", "Dawarich", `href="/week?scope=`} {
		if !strings.Contains(page, want) {
			t.Fatalf("week page lacks %q:\n%s", want, page)
		}
	}
}

// Without a Verbund the EÜR bench leads to the categories of its
// Invoice Ninja alone, which save there.
func TestCategoriesWithoutVerbund(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
		"service": {"invoiceninja"}, "name": {"Ninja"}, "url": {"demo://invoiceninja"}, "mode": {"shared"}, "tls": {"verify"}})
	page := string(mustGet(t, srv, client, "/euer"))
	m := regexp.MustCompile(`href="(/categories\?ninja=\d+&amp;bank=0)"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatalf("no categories link:\n%s", page)
	}
	link := strings.ReplaceAll(m[1], "&amp;", "&")
	cats := string(mustGet(t, srv, client, link))
	if !strings.Contains(cats, `<option value="3" selected>EÜR</option>`) || !strings.Contains(cats, `action="`+m[1]+`"`) {
		t.Fatalf("categories:\n%s", cats)
	}
	resp := postForm(t, client, srv.URL+link, url.Values{"csrf": {csrfToken(t, srv, client)}, "euer_slot": {"3"}})
	if loc := resp.Header.Get("Location"); !strings.Contains(loc, "verbund.categories_saved") {
		t.Fatalf("save: %s", loc)
	}
	if page := string(mustGet(t, srv, client, "/euer")); !strings.Contains(page, `name="act_`) {
		t.Fatalf("euer after setup:\n%s", page)
	}
}

// The reminders page opens on a demo Invoice Ninja.
func TestDunningPage(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
		"service": {"invoiceninja"}, "name": {"Ninja"}, "url": {"demo://invoiceninja"}, "mode": {"shared"}, "tls": {"verify"}})
	if page := string(mustGet(t, srv, client, "/dunning")); !strings.Contains(page, "Mahnlauf") || strings.Contains(page, "dunning.") {
		t.Fatalf("dunning page:\n%s", page)
	}
}

// The subscriptions page lists the demo's cases and reviews a new price.
func TestSubsPage(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	for _, service := range []string{"sure", "wallos", "invoiceninja"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
			"service": {service}, "name": {service}, "url": {"demo://" + service}, "mode": {"shared"}, "tls": {"verify"}})
	}
	page := string(mustGet(t, srv, client, "/subs"))
	for _, want := range []string{"Abos", `name="act_np-demo-r1" value="ninja_price"`, `name="amount_np-demo-r1" value="44.90"`, "Doppelt in Wallos"} {
		if !strings.Contains(page, want) {
			t.Fatalf("subs page lacks %q:\n%s", want, page)
		}
	}
	resp, err := client.PostForm(srv.URL+"/subs/review", url.Values{"csrf": {csrfToken(t, srv, client)}, "pick": {"np-demo-r1"},
		"act_np-demo-r1": {"ninja_price"}, "amount_np-demo-r1": {"44.90"}})
	if err != nil {
		t.Fatal(err)
	}
	if review := readBody(t, resp); !strings.Contains(review, "Abos bestätigen") || !strings.Contains(review, `name="amount_np-demo-r1" value="44.90"`) {
		t.Fatalf("review:\n%s", review)
	}
}

// The filing page lists the demo's merchant without category.
func TestCategorizePage(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
		"service": {"sure"}, "name": {"Sure"}, "url": {"demo://sure"}, "mode": {"shared"}, "tls": {"verify"}})
	page := string(mustGet(t, srv, client, "/categorize"))
	for _, want := range []string{"Buchungen einordnen", `value="file"`, `<option value="Technik" selected>Technik</option>`} {
		if !strings.Contains(page, want) {
			t.Fatalf("categorize page lacks %q:\n%s", want, page)
		}
	}
}

// The year page sums the demo's year and offers the package; Snipe-IT
// adds the depreciation of its assets.
func TestYearPage(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	for _, service := range []string{"invoiceninja", "kimai", "snipeit"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
			"service": {service}, "name": {service}, "url": {"demo://" + service}, "mode": {"shared"}, "tls": {"verify"}})
	}
	page := string(mustGet(t, srv, client, "/year?year="+strconv.Itoa(time.Now().Year())))
	for _, want := range []string{"Ausgaben je EÜR-Zeile", "Abschreibung", "Schnitt-Laptop", "Tage mit Arbeit in Kimai", `action="/billing/export"`, `name="space_id" value="` + space + `"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("year page lacks %q:\n%s", want, page)
		}
	}
}

// The updates page lists the demo's newer tags, one from a release
// feed, and reviews a pull request.
func TestUpdatesPage(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	for _, service := range []string{"gitea", "wud", "freshrss"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
			"service": {service}, "name": {service}, "url": {"demo://" + service}, "mode": {"shared"}, "tls": {"verify"}})
	}
	page := string(mustGet(t, srv, client, "/updates"))
	m := regexp.MustCompile(`name="tag_(u-[a-z0-9-]*dawarich[a-z0-9-]*)" value="0.30.9"`).FindStringSubmatch(page)
	if m == nil || !strings.Contains(page, "Pull Request") {
		t.Fatalf("updates page:\n%s", page)
	}
	resp, err := client.PostForm(srv.URL+"/updates/review", url.Values{"csrf": {csrfToken(t, srv, client)}, "pick": {m[1]},
		"act_" + m[1]: {"propose"}, "tag_" + m[1]: {"0.30.9"}})
	if err != nil {
		t.Fatal(err)
	}
	if review := readBody(t, resp); !strings.Contains(review, "Pull Requests bestätigen") || !strings.Contains(review, "0.30.6 → 0.30.9") {
		t.Fatalf("review:\n%s", review)
	}
}

// The inventory page lists the demo network's unknown devices with
// Snipe-IT's models.
func TestInventoryPage(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	for _, service := range []string{"snipeit", "fritzbox"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
			"service": {service}, "name": {service}, "url": {"demo://" + service}, "mode": {"shared"}, "tls": {"verify"}})
	}
	page := string(mustGet(t, srv, client, "/inventory"))
	for _, want := range []string{"Inventur", `value="add"`, `<option value="Sonstiges Netzwerkgerät">`, `list="inventory-models"`, "FRITZ!Box"} {
		if !strings.Contains(page, want) {
			t.Fatalf("inventory page lacks %q:\n%s", want, page)
		}
	}
}
