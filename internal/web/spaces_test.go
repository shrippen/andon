package web_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestSpaceSettingsSaveGoalsAndRules(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	csrf := csrfToken(t, srv, client)

	resp := getFollowingRedirect(t, srv, client, "/spaces/settings?section=rules")
	page := readAll(t, resp)
	resp.Body.Close()
	rulesURL := resp.Request.URL.Path
	financeURL := strings.TrimSuffix(rulesURL, "rules") + "finance"
	if !strings.Contains(page, `name="rule.kimai.timer_running_long.hours"`) {
		t.Fatalf("rule parameters missing:\n%s", page)
	}

	// Each section saves alone and keeps the others' values.
	for _, save := range []struct {
		url  string
		form url.Values
	}{
		{financeURL, url.Values{"csrf": {csrf}, "revenue_year": {"90000"}, "vat_method": {"soll"}, "billing_internal": {"Intern, Verein"}}},
		{rulesURL, url.Values{"csrf": {csrf}, "rule.kimai.timer_running_long.hours": {"6"}, "center": {"median"}}},
	} {
		if resp := postForm(t, client, srv.URL+save.url, save.form); resp.StatusCode != http.StatusSeeOther {
			t.Fatalf("save %s: %d", save.url, resp.StatusCode)
		}
	}
	page = string(mustGet(t, srv, client, financeURL)) + string(mustGet(t, srv, client, rulesURL))
	for _, want := range []string{`value="90000"`, `<option value="soll" selected>`, `name="rule.kimai.timer_running_long.hours" value="6"`,
		`name="billing_internal" value="Intern, Verein"`, `<option value="median" selected>`} {
		if !strings.Contains(page, want) {
			t.Fatalf("expected %q after save:\n%s", want, page)
		}
	}
	if strings.Contains(page, `name="rule.kimai.timer_running_long.enabled" checked`) {
		t.Fatal("an unchecked rule must be stored as disabled")
	}
}
