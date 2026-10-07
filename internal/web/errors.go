package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"andon/internal/services/access"
	"andon/internal/services/admin"
	"andon/internal/services/audit"
	"andon/internal/services/clients"
	"andon/internal/services/hints"
	"andon/internal/services/hosts"
	"andon/internal/services/invites"
	"andon/internal/services/notify"
	"andon/internal/services/oidc"
	"andon/internal/services/receipts"
	"andon/internal/services/selfbackup"
	"andon/internal/services/system"
	"andon/internal/services/util"

	"andon/internal/services/accounts"
	"andon/internal/services/auth"
	"andon/internal/services/shares"
	"andon/internal/services/teams"
	"andon/internal/services/widgetlib"
)

// knownErrors maps service errors without a catalog-key message to their
// catalog key. Errors whose message already is a key pass through.
var knownErrors = []struct {
	err error
	key string
}{
	{accounts.ErrEmailTaken, "account.email_taken"},
	{accounts.ErrPasswordTooShort, "password.too_short"},
	{accounts.ErrWrongPassword, "password.wrong"},
	{auth.ErrThrottled, "login.throttled"},
	{auth.ErrOIDCOnly, "login.oidc_only"},
	{auth.ErrTOTPInvalid, "totp.invalid"},
	{teams.ErrNameMissing, "team.name_missing"},
	{teams.ErrNameTaken, "team.name_taken"},
	{teams.ErrNotFound, "team.not_found"},
	{teams.ErrDenied, "error.denied"},
	{shares.ErrRight, "share.right_invalid"},
	{widgetlib.ErrUnknownType, "widget.unknown_type"},
	{widgetlib.ErrConnRequired, "widget.connection_required"},
	{widgetlib.ErrConnMissing, "widget.connection_missing"},
	{widgetlib.ErrConnWrongService, "widget.connection_type"},
	{util.ErrConflict, "error.conflict"},
	{notify.ErrFailed, "notify.failed"},
	{notify.ErrInvalidURL, "notify.invalid_url"},
	{notify.ErrBadTime, "notify.bad_time"},
}

// errKey returns the catalog key for err, for {{t .Error}} in templates.
// e.g. accounts.ErrEmailTaken -> "account.email_taken".
func errKey(err error) string {
	for _, k := range knownErrors {
		if errors.Is(err, k.err) {
			return k.key
		}
	}
	return err.Error()
}

// Denied and not-found errors of the services, answered 403 and 404
// whatever the handler expected.
var (
	deniedErrors = []error{access.ErrDenied, admin.ErrDenied, audit.ErrDenied, auth.ErrForbidden, hints.ErrDenied,
		invites.ErrDenied, oidc.ErrDenied, selfbackup.ErrDenied, system.ErrDenied, teams.ErrDenied}
	notFoundErrors = []error{util.ErrNotFound, admin.ErrNotFound, clients.ErrNotFound, hints.ErrNotFound, hosts.ErrNotFound,
		receipts.ErrNotFound, shares.ErrNotFound, teams.ErrNotFound}
)

// fail answers a failed request: 403 denied, 404 not found, 409 stale
// version, else fallback. A 400 names the error (a catalog key, see
// errKey); a 500 is logged and says nothing more, as its text may name
// tables, hosts or paths.
func (d Deps) fail(w http.ResponseWriter, err error, fallback int) {
	status := fallback
	switch {
	case isAny(err, deniedErrors):
		status = http.StatusForbidden
	case isAny(err, notFoundErrors):
		status = http.StatusNotFound
	case errors.Is(err, util.ErrConflict):
		status = http.StatusConflict
	}

	if status >= http.StatusInternalServerError {
		slog.Error("request failed", "err", err)
		http.Error(w, http.StatusText(status), status)
		return
	}
	if status != fallback {
		http.Error(w, http.StatusText(status), status)
		return
	}
	http.Error(w, errKey(err), status)
}

func isAny(err error, list []error) bool {
	for _, e := range list {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

// formInt reads a whole-number form field; 0 when missing or not a
// number (fields where 0 means "none", or a version a stale form sends).
func formInt(r *http.Request, name string) int {
	n, _ := strconv.Atoi(r.FormValue(name))
	return n
}

// formID reads an id form field; 0 when missing or not a number.
func formID(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(r.FormValue(name), 10, 64)
	return n
}
