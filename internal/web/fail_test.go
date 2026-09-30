package web

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"andon/internal/services/access"
	"andon/internal/services/admin"
	"andon/internal/services/hints"
	"andon/internal/services/teams"
	"andon/internal/services/util"
)

// TestFailStatus: a denied request answers 403, a missing object 404, a
// stale version 409, and an internal error 500 without its text (it may
// name tables or hosts).
func TestFailStatus(t *testing.T) {
	cases := []struct {
		err      error
		fallback int
		want     int
	}{
		{access.ErrDenied, http.StatusInternalServerError, http.StatusForbidden},
		{fmt.Errorf("delete: %w", access.ErrDenied), http.StatusBadRequest, http.StatusForbidden},
		{teams.ErrDenied, http.StatusInternalServerError, http.StatusForbidden},
		{admin.ErrDenied, http.StatusInternalServerError, http.StatusForbidden},
		{hints.ErrNotFound, http.StatusInternalServerError, http.StatusNotFound},
		{util.ErrNotFound, http.StatusBadRequest, http.StatusNotFound},
		{util.ErrConflict, http.StatusInternalServerError, http.StatusConflict},
		{errors.New("sql: no such table secrets"), http.StatusInternalServerError, http.StatusInternalServerError},
		{errors.New("form.name_missing"), http.StatusBadRequest, http.StatusBadRequest},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		(Deps{}).fail(rec, c.err, c.fallback)
		if rec.Code != c.want {
			t.Errorf("%v: %d, want %d", c.err, rec.Code, c.want)
		}
		if strings.Contains(rec.Body.String(), "sql:") {
			t.Errorf("%v: internal text shown: %q", c.err, rec.Body.String())
		}
	}
}
