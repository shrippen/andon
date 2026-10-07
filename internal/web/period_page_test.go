package web_test

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// moneyDemo connects the demo Kimai and Invoice Ninja and runs the
// analysis once.
func moneyDemo(t *testing.T) func(string) string {
	t.Helper()
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)
	space := regexp.MustCompile(`<option value="(\d+)"`).FindSubmatch(mustGet(t, srv, client, "/connections/new?service=kimai"))[1]
	for _, svc := range []string{"kimai", "invoiceninja"} {
		postForm(t, client, srv.URL+"/connections", url.Values{"csrf": {csrf}, "service": {svc}, "space_id": {string(space)},
			"name": {svc}, "url": {"demo://" + svc}, "mode": {"shared"}, "secret": {"demo"}, "tls": {"verify"}})
	}
	runAnalysis(t, srv)
	return func(path string) string { return string(mustGet(t, srv, client, path)) }
}

// periodChips checks a page's period choice: a segmented GET form to
// path with three buttons, the asked one pressed.
func periodChips(t *testing.T, page, path, pressed string) {
	t.Helper()
	form := regexp.MustCompile(`(?s)<form method="get" action="` + regexp.QuoteMeta(path) + `" class="seg"[^>]*>.*?</form>`).FindString(page)
	if form == "" {
		t.Fatalf("no period choice for %s:\n%s", path, page)
	}
	for _, p := range []string{"12m", "year", "prev"} {
		button := regexp.MustCompile(`<button[^>]*name="period" value="` + p + `"[^>]*>`).FindString(form)
		if button == "" {
			t.Fatalf("no %s button: %s", p, form)
		}
		if strings.Contains(button, `aria-pressed="true"`) != (p == pressed) {
			t.Fatalf("button %s pressed wrong (want %s): %s", p, pressed, button)
		}
	}
}

// TestClientsPeriod: the customer pages count hours and revenue over the
// chosen period, the last 12 months by default, and every figure names
// its span; figures as of today say so.
func TestClientsPeriod(t *testing.T) {
	get := moneyDemo(t)
	year := time.Now().Year()

	list := get("/clients")
	periodChips(t, list, "/clients", "12m")
	if !strings.Contains(list, "letzte 12 Monate") {
		t.Fatalf("list does not name its period:\n%s", list)
	}
	prev := get("/clients?period=prev")
	periodChips(t, prev, "/clients", "prev")
	if !strings.Contains(prev, strconv.Itoa(year-1)) {
		t.Fatalf("last year's list does not name %d:\n%s", year-1, prev)
	}

	// The customer links keep the period.
	link := regexp.MustCompile(`href="(/clients/\d+/\d+\?kimai=\d+&amp;period=prev)"`).FindStringSubmatch(prev)
	if link == nil {
		t.Fatalf("customer links lose the period:\n%s", prev)
	}
	detail := get(strings.ReplaceAll(link[1], "&amp;", "&"))
	path := strings.SplitN(strings.ReplaceAll(link[1], "&amp;", "&"), "?", 2)[0]
	periodChips(t, detail, path, "prev")
	if !regexp.MustCompile(`class="seg"[^>]*><input type="hidden" name="kimai" value="\d+">`).MatchString(detail) {
		t.Fatalf("the period choice loses the Kimai connection:\n%s", detail)
	}
	subs := regexp.MustCompile(`<span class="kpi-sub">([^<]*)</span>`).FindAllStringSubmatch(detail, -1)
	var named []string
	for _, s := range subs {
		named = append(named, s[1])
	}
	joined := strings.Join(named, " | ")
	if strings.Count(joined, strconv.Itoa(year-1)) < 2 || !strings.Contains(joined, "Stand heute") {
		t.Fatalf("figures do not name their span: %s", joined)
	}
	if resp := get("/clients?period=year"); !strings.Contains(resp, strconv.Itoa(year)) {
		t.Fatalf("this year's list does not name %d", year)
	}
}

// TestBillingPeriod: the billing page sums the chosen period (last 12
// months by default) and names it under every figure; the unbilled sum
// is as of today, and the tax package offers the chosen year first.
func TestBillingPeriod(t *testing.T) {
	get := moneyDemo(t)
	year := time.Now().Year()

	page := get("/billing")
	periodChips(t, page, "/billing", "12m")
	figures := regexp.MustCompile(`(?s)<div class="client-kpis" id="figures">.*?</div>\s*</div>`).FindString(page)
	if strings.Count(figures, "letzte 12 Monate") < 4 {
		t.Fatalf("figures do not name their span:\n%s", figures)
	}
	if !strings.Contains(page, "Stand heute") {
		t.Fatalf("unbilled sum does not say it is as of today:\n%s", page)
	}

	page = get("/billing?period=prev")
	periodChips(t, page, "/billing", "prev")
	figures = regexp.MustCompile(`(?s)<div class="client-kpis" id="figures">.*?</div>\s*</div>`).FindString(page)
	if strings.Count(figures, strconv.Itoa(year-1)) < 4 {
		t.Fatalf("last year's figures do not name %d:\n%s", year-1, figures)
	}
	if !strings.Contains(page, `<option selected>`+strconv.Itoa(year-1)+`</option>`) {
		t.Fatalf("tax package does not offer %d first:\n%s", year-1, page)
	}
}
