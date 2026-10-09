package connections_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"andon/internal/enums"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/connections"
)

// TestCauseOf: known transport and status failures get a cause, wrapped
// or not; anything else none.
func TestCauseOf(t *testing.T) {
	cases := map[string]connections.Cause{
		"connection refused: kimai.lan":             connections.CauseRefused,
		"kimai: egress denied: 10.0.0.5":            connections.CauseEgress,
		"HTTP 401":                                  connections.CauseAuth,
		"kimai: HTTP 403":                           connections.CauseForbidden,
		"tls certificate: kimai.lan":                connections.CauseTLS,
		"dns: kimai.lan":                            connections.CauseDNS,
		"timeout: kimai.lan":                        connections.CauseTimeout,
		"HTTP 500":                                  connections.CauseNone,
		"invalid JSON":                              connections.CauseNone,
		"HTTP 4010 is no status, but contains none": connections.CauseNone,
	}
	for msg, want := range cases {
		if got := connections.CauseOf(msg); got != want {
			t.Errorf("%q: %q, want %q", msg, got, want)
		}
	}
}

// TestMovedTo: a redirect to another server names its target, the
// address to move the connection to.
func TestMovedTo(t *testing.T) {
	msg := "ghostfolio: redirected to https://g.example: use it as the URL"
	if got := connections.CauseOf(msg); got != connections.CauseMoved {
		t.Fatalf("cause: %q", got)
	}
	if got := connections.MovedTo(msg); got != "https://g.example" {
		t.Fatalf("target: %q", got)
	}
	if got := connections.MovedTo("HTTP 401"); got != "" {
		t.Fatalf("no redirect: %q", got)
	}
}

// TestTestNamesCause: a failed test carries the cause beside the raw text.
func TestTestNamesCause(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	d := openTestDB(t)
	u := addUser(t, d, "a@b.c")
	who, _ := access.Load(d, u.ID)
	space, _ := content.PersonalSpace(d, u.ID)
	id, err := connections.Create(d, who, space.ID, enums.ServiceKimai, "Kimai", srv.URL,
		enums.CredentialShared, "tok", connections.TLSVerify, nil)
	if err != nil {
		t.Fatal(err)
	}

	result, err := connections.Test(context.Background(), d, who, id)
	if err != nil {
		t.Fatal(err)
	}
	if result.Ok || result.Cause != connections.CauseAuth || result.Message == "" {
		t.Fatalf("result: %+v", result)
	}
}
