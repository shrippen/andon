package web

import (
	"bytes"
	"net/http"
	"net/url"
	"strings"

	"andon/internal/i18n"
)

// errorBodyMax caps the caught text of an error answer; it is a status
// text or a catalog key, never more.
const errorBodyMax = 4096

// bareErrorPaths answer machines (API, feeds, embeds), assets and
// fragments: their errors stay plain text even when a browser asks.
var bareErrorPaths = []string{"/api/", "/static/", "/theme/", "/theme-fonts/", "/icons/", "/embed/",
	"/calendar.ics", "/healthz", "/widget-fragments/", "/widget-tiles/", "/widget-sample/"}

// framedKinds are the statuses with their own title and lead in the
// catalog (error_page.<kind>.*); others use error_page.other.
var framedKinds = map[int]string{
	http.StatusBadRequest: "400", http.StatusForbidden: "403", http.StatusNotFound: "404",
	http.StatusMethodNotAllowed: "405", http.StatusConflict: "409", http.StatusInternalServerError: "500",
}

// errorPages frames the plain-text error answers (http.Error, the mux's
// 404 and 405) of a browser page load in the app: header, message, a way
// back. The status stays. htmx swaps and API, feed and asset requests
// keep the bare answer.
//
//	handler ── http.Error(403, "csrf") ──▶ errorWriter (held back)
//	                                          └──▶ error_page, 403
func (d Deps) errorPages(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isPageLoad(r) {
			next.ServeHTTP(w, r)
			return
		}

		ew := &errorWriter{ResponseWriter: w}
		next.ServeHTTP(ew, r)
		if ew.status == 0 {
			return
		}
		d.errorPage(w, r, ew.status, strings.TrimSpace(ew.body.String()))
	})
}

// isPageLoad: a browser loads or posts a whole page (not htmx, not an
// API client, not an asset).
func isPageLoad(r *http.Request) bool {
	if r.Header.Get("HX-Request") != "" || !strings.Contains(r.Header.Get("Accept"), "text/html") {
		return false
	}
	for _, p := range bareErrorPaths {
		if strings.HasPrefix(r.URL.Path, p) {
			return false
		}
	}
	return true
}

// errorPage renders the framed page; if that fails, the plain text.
func (d Deps) errorPage(w http.ResponseWriter, r *http.Request, status int, text string) {
	ctx, info, err := d.resolve(r)
	if err != nil {
		ctx = Ctx{Locale: i18n.Pick(r.Header.Get("Accept-Language")), Path: r.URL.Path}
	}
	// A session still waiting for its second factor sees no navigation.
	if info != nil && info.Pending2FA {
		ctx.Who = nil
	}

	kind, ok := framedKinds[status]
	if !ok {
		kind = "other"
	}
	values := map[string]any{"Kind": kind, "Status": status, "Detail": errorDetail(text), "Back": refererPath(r)}
	if d.Page(w, ctx, "error_page", status, values) != nil {
		http.Error(w, text, status)
	}
}

// errorDetail is the catalog key an error answer names: the text itself
// ("error.conflict", "form.name_missing") or under error. ("csrf"); ""
// for a bare status text ("Not Found").
func errorDetail(text string) string {
	switch {
	case text == "":
		return ""
	case i18n.Has(text):
		return text
	case i18n.Has("error." + text):
		return "error." + text
	}
	return ""
}

// refererPath is the page the request came from on this site, else the
// start page; the way back without JavaScript.
func refererPath(r *http.Request) string {
	ref, err := url.Parse(r.Referer())
	if err != nil || ref.Host != r.Host || !strings.HasPrefix(ref.Path, "/") || strings.HasPrefix(ref.Path, "//") {
		return "/"
	}
	if ref.RawQuery != "" {
		return ref.Path + "?" + ref.RawQuery
	}
	return ref.Path
}

// errorWriter holds back a plain-text (or untyped) error answer for
// errorPages and passes every other answer through.
type errorWriter struct {
	http.ResponseWriter
	status int // caught error status; 0 while passing through
	passed bool
	body   bytes.Buffer
}

func (e *errorWriter) WriteHeader(code int) {
	if e.passed || e.status != 0 {
		return
	}
	if code < http.StatusOK {
		e.ResponseWriter.WriteHeader(code) // 1xx, e.g. early hints
		return
	}

	kind := e.Header().Get("Content-Type")
	if code >= http.StatusBadRequest && (kind == "" || strings.HasPrefix(kind, "text/plain")) {
		e.status = code
		return
	}
	e.passed = true
	e.ResponseWriter.WriteHeader(code)
}

func (e *errorWriter) Write(b []byte) (int, error) {
	if !e.passed && e.status == 0 {
		e.WriteHeader(http.StatusOK)
	}
	if e.passed {
		return e.ResponseWriter.Write(b)
	}

	if room := errorBodyMax - e.body.Len(); room > 0 {
		e.body.Write(b[:min(len(b), room)])
	}
	return len(b), nil
}

// Flush passes through only once the answer is not held back.
func (e *errorWriter) Flush() {
	if !e.passed {
		return
	}
	if f, ok := e.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (e *errorWriter) Unwrap() http.ResponseWriter { return e.ResponseWriter }
