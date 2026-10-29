package web_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestVATPage: without Invoice Ninja the page says so; with the demo it
// lists the open transactions, the review spells out the writes, and
// the demo refuses to write them.
func TestVATPage(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	page := string(mustGet(t, srv, client, "/vat"))
	if !strings.Contains(page, "Invoice Ninja fehlt") {
		t.Fatalf("no hint about the missing connection:\n%s", page)
	}

	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	for _, service := range []string{"invoiceninja", "sure"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
			"service": {service}, "name": {service}, "url": {"demo://" + service}, "mode": {"shared"}, "tls": {"verify"}})
	}

	month := time.Now().UTC().Format("2006-01")
	page = string(mustGet(t, srv, client, "/vat?span="+month))
	for _, want := range []string{"Offene Umsätze", "Kabelwerk Studiobedarf", `name="act_demo-b-studio"`, "data-vat-bench", `class="bench-entry"`, `data-desk="demo-b-studio"`,
		`name="note_demo-b-studio"`, "Zahllast", "ELSTER-Kennzahlen", "Vorauszahlung (Zahllast)", "data-copy="} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %q:\n%s", want, page)
		}
	}

	form := url.Values{"csrf": {csrfToken(t, srv, client)}, "span": {month}, "pick": {"demo-b-studio"},
		"act_demo-b-studio": {"create"}, "vendor_demo-b-studio": {"Kabelwerk Studiobedarf"}, "tax_demo-b-studio": {"19"},
		"num_demo-b-studio": {"KW-56107"}, "note_demo-b-studio": {"Lichtstative"}, "bill_demo-b-studio": {"on", "off"},
		"paid_demo-b-studio": {"off"}, "gross_demo-b-studio": {"off"}}
	resp, err := client.PostForm(srv.URL+"/vat/review", form)
	if err != nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("review: %v %v", resp, err)
	}
	review := readBody(t, resp)
	if !strings.Contains(review, "Ausgabe „Kabelwerk Studiobedarf“ anlegen") || !strings.Contains(review, "Vorsteuer 143,54") {
		t.Fatalf("review lacks the step:\n%s", review)
	}
	for _, want := range []string{"Rechnungsnr. KW-56107", "„Lichtstative“", "in Rechnung stellen", "nicht bezahlt", "Betrag ohne Steuer"} {
		if !strings.Contains(review, want) {
			t.Fatalf("review lacks %q:\n%s", want, review)
		}
	}

	// The review sends back what it showed (bench_step).
	for _, want := range []string{`name="pick" value="demo-b-studio"`, `name="act_demo-b-studio" value="create"`,
		`name="vendor_demo-b-studio" value="Kabelwerk Studiobedarf"`, `name="tax_demo-b-studio" value="19"`} {
		if !strings.Contains(review, want) {
			t.Fatalf("review lacks %s:\n%s", want, review)
		}
	}

	// "Archive all as private" takes the private accounts' rest only.
	all := url.Values{"csrf": {csrfToken(t, srv, client)}, "span": {month}, "all_private": {"1"}, "apart": {"demo-p-food"},
		"pick": {"demo-b-studio"}, "act_demo-b-studio": {"create"}}
	resp, err = client.PostForm(srv.URL+"/vat/review", all)
	if err != nil {
		t.Fatal(err)
	}
	if review := readBody(t, resp); !strings.Contains(review, "Als privat in Invoice Ninja archivieren") || strings.Contains(review, "Kabelwerk") {
		t.Fatalf("archive review:\n%s", review)
	}

	resp = postForm(t, client, srv.URL+"/vat/apply", form)
	if loc := resp.Header.Get("Location"); resp.StatusCode != http.StatusSeeOther || !strings.Contains(loc, "vatcheck.err_demo") {
		t.Fatalf("apply on the demo: %d %s", resp.StatusCode, loc)
	}
}

// TestVerbundAccounts: the settled Verbund lists the demo's accounts;
// a role set there reaches /vat.
func TestVerbundAccounts(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	for _, service := range []string{"invoiceninja", "sure"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
			"service": {service}, "name": {service}, "url": {"demo://" + service}, "mode": {"shared"}, "tls": {"verify"}})
	}
	resp := postForm(t, client, srv.URL+"/spaces/"+space+"/verbund/settle", url.Values{"csrf": {csrfToken(t, srv, client)}})
	link := regexp.MustCompile(`/verbund/(\d+)/`).FindStringSubmatch(resp.Header.Get("Location"))
	if link == nil {
		t.Fatalf("settle: %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}

	page := string(mustGet(t, srv, client, "/verbund/"+link[1]+"/accounts?space="+space))
	for _, want := range []string{"Geschäftskonto", "Privatkonto", "Tagesgeld", `name="role"`} {
		if !strings.Contains(page, want) {
			t.Fatalf("accounts page lacks %q:\n%s", want, page)
		}
	}
	conn := regexp.MustCompile(`name="conn" value="(\d+)"><input type="hidden" name="key" value="demo-a2"`).FindStringSubmatch(page)
	if conn == nil {
		t.Fatalf("no role form for the private account:\n%s", page)
	}
	resp = postForm(t, client, srv.URL+"/verbund/"+link[1]+"/accounts/role", url.Values{"csrf": {csrfToken(t, srv, client)}, "space": {space},
		"conn": {conn[1]}, "key": {"demo-a2"}, "role": {"private"}})
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("role: %d", resp.StatusCode)
	}
	vat := string(mustGet(t, srv, client, "/vat?span="+time.Now().UTC().Format("2006-01")))
	if !regexp.MustCompile(`name="role:` + conn[1] + `:demo-a2"[^>]*>\s*<option value="">[^<]*</option>\s*<option value="business">[^<]*</option>\s*<option value="private" selected`).MatchString(vat) {
		t.Fatalf("/vat does not show the role:\n%s", vat)
	}
}

// TestVATScanOffer: a debit without expense, whose scan waits in
// Paperless, is offered the scan for its new expense.
func TestVATScanOffer(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	for _, service := range []string{"invoiceninja", "paperless"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
			"service": {service}, "name": {service}, "url": {"demo://" + service}, "mode": {"shared"}, "tls": {"verify"}})
	}
	now := time.Now().UTC()
	var scan, receipt bool
	for _, month := range []string{now.Format("2006-01"), now.AddDate(0, -1, 0).Format("2006-01")} {
		page := string(mustGet(t, srv, client, "/vat?span="+month))
		if !scan && strings.Contains(page, `name="scan_demo-b-scan"`) {
			scanSearch(t, srv, client, page)
			scan = true
		}

		// A scan no booking claims gets a desk of its own, its amount editable.
		if k := regexp.MustCompile(`data-desk="(d-\d+)"`).FindStringSubmatch(page); k != nil {
			receipt = strings.Contains(page, `name="amount_`+k[1]+`"`) && strings.Contains(page, `name="day_`+k[1]+`"`)
		}
	}
	if !scan {
		t.Fatal("no scan offered for the debit of a waiting scan")
	}
	if !receipt {
		t.Fatal("no desk for a receipt without an expense")
	}
}

// scanSearch: the desk's search finds Paperless scans to pick, and an
// empty one says so; a gap case's search names its radios xscan.
func scanSearch(t *testing.T, srv *httptest.Server, client *http.Client, page string) {
	t.Helper()
	found := string(mustGet(t, srv, client, "/vat/scans?case=demo-b-scan&q_scan_demo-b-scan=Rechnung"))
	if !strings.Contains(found, `name="scan_demo-b-scan"`) || !strings.Contains(found, "/receipts/thumb/") {
		t.Fatalf("search offers no scan:\n%s", found)
	}
	if none := string(mustGet(t, srv, client, "/vat/scans?case=demo-b-scan&q_scan_demo-b-scan=")); !strings.Contains(none, "Nichts gefunden") {
		t.Fatalf("empty search:\n%s", none)
	}

	gap := regexp.MustCompile(`data-desk="(x-[^"]+)"`).FindStringSubmatch(page)
	if gap == nil {
		t.Fatal("no expense with gaps on the page")
	}
	found = string(mustGet(t, srv, client, "/vat/scans?field=xscan&case="+gap[1]+"&q_xscan_"+gap[1]+"=Rechnung"))
	if !strings.Contains(found, `name="xscan_`+gap[1]+`"`) {
		t.Fatalf("gap search offers no scan:\n%s", found)
	}
}

// Two Invoice Ninjas: the page asks which to work in, and a pick sticks
// to the page's links.
func TestVATScopes(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	var ids []string
	for _, name := range []string{"Studio", "Verein"} {
		resp := postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
			"service": {"invoiceninja"}, "name": {name}, "url": {"demo://invoiceninja"}, "mode": {"shared"}, "tls": {"verify"}})
		ids = append(ids, regexp.MustCompile(`/connections/(\d+)`).FindStringSubmatch(resp.Header.Get("Location"))[1])
	}

	page := string(mustGet(t, srv, client, "/vat"))
	for _, want := range []string{"Mehrere Verbünde passen", `name="scope"`, `<option value="c` + ids[1] + `">Verein</option>`} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %q:\n%s", want, page)
		}
	}
	page = string(mustGet(t, srv, client, "/vat?scope=c"+ids[1]))
	if strings.Contains(page, "Mehrere Verbünde passen") || !strings.Contains(page, `href="/vat?scope=c`+ids[1]+`&amp;span=`) {
		t.Fatalf("picked scope:\n%s", page)
	}
}

// TestVATSplit: a debit offers "split"; the review lists each part, a
// private one without an expense, and refuses parts that miss the sum.
func TestVATSplit(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)

	space := string(regexp.MustCompile(`<option value="(\d+)">`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1])
	postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrfToken(t, srv, client)}, "space_id": {space},
		"service": {"invoiceninja"}, "name": {"invoiceninja"}, "url": {"demo://invoiceninja"}, "mode": {"shared"}, "tls": {"verify"}})

	month := time.Now().UTC().Format("2006-01")
	page := string(mustGet(t, srv, client, "/vat?span="+month))
	for _, want := range []string{`data-set-act="split"`, "data-split", `name="s0.use_demo-b-studio"`, `name="sgross_demo-b-studio"`, "data-split-bar"} {
		if !strings.Contains(page, want) {
			t.Fatalf("page lacks %q:\n%s", want, page)
		}
	}

	split := func(rest string) string {
		form := url.Values{"csrf": {csrfToken(t, srv, client)}, "span": {month}, "pick": {"demo-b-studio"},
			"act_demo-b-studio": {"split"}, "sgross_demo-b-studio": {"on", "off"}, "spaid_demo-b-studio": {"on", "off"},
			"s0.use_demo-b-studio": {"expense"}, "s0.vendor_demo-b-studio": {"Kabelwerk Studiobedarf"}, "s0.tax_demo-b-studio": {"19"},
			"s0.amount_demo-b-studio": {"800,00"}, "s1.use_demo-b-studio": {"private"}, "s1.amount_demo-b-studio": {rest},
			"s1.note_demo-b-studio": {"Kaffeemaschine"}}
		resp, err := client.PostForm(srv.URL+"/vat/review", form)
		if err != nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("review: %v %v", resp, err)
		}
		return readBody(t, resp)
	}
	review := split("99,00")
	for _, want := range []string{"Umsatz aufteilen auf 2 Teile", "Ausgabe „Kabelwerk Studiobedarf“", "800,00", "Privat, keine Ausgabe: 99,00", "„Kaffeemaschine“",
		`name="s1.use_demo-b-studio" value="private"`} {
		if !strings.Contains(review, want) {
			t.Fatalf("review lacks %q:\n%s", want, review)
		}
	}
	if review := split("98,00"); !strings.Contains(review, "Die Teile ergeben nicht den Betrag des Umsatzes") {
		t.Fatalf("a short split is not refused:\n%s", review)
	}
}
