package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"andon/internal/sources"
)

// TestCalendarConnLogin: the secret is a private address, or a login
// ("user:password") for the connection's own address, which may change
// later without entering the login again.
func TestCalendarConnLogin(t *testing.T) {
	var path, user, pass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		user, pass, _ = r.BasicAuth()
		w.Write([]byte("BEGIN:VCALENDAR\r\nEND:VCALENDAR\r\n"))
	}))
	defer srv.Close()

	cases := []struct{ secret, path, user, pass string }{
		{"", "/public", "", ""},
		{"anna:p@ss:w", "/public", "anna", "p@ss:w"},
		{srv.URL + "/private", "/private", "", ""},
	}
	for _, c := range cases {
		path, user, pass = "", "", ""
		if _, err := sources.CalendarData.Fetch(context.Background(), sources.Ctx{URL: srv.URL + "/public", Secret: c.secret}); err != nil {
			t.Fatalf("%q: %v", c.secret, err)
		}
		if path != c.path || user != c.user || pass != c.pass {
			t.Errorf("%q: got %s %q:%q", c.secret, path, user, pass)
		}
	}
}
