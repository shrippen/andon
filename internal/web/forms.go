package web

// Forms keep what was typed when the server refuses them, and say once
// what a form did after its redirect. One mechanism for every form:
//
//	POST ─► refused (≥ 400) ─► page rendered ─► keepInput: the fields of
//	        the posted form get the typed values back (never secrets)
//	GET with a query (a get form, e.g. the year package) ─► refused:
//	        the same for the form whose method is get
//	POST ─► done ─► flash(w, key) + 303 ─► next page: toast, once

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"andon/internal/i18n"
)

// secretParts mark a field whose value never comes back: a name made of
// one of them ("password", "client_secret", "opt_api_key", "code").
var secretParts = map[string]bool{
	"password": true, "passwd": true, "secret": true, "token": true, "key": true, "code": true,
	"csrf": true, "current": true, "new": true, "otp": true, "totp": true, "recovery": true,
}

// secretAttr marks an input that holds a secret under a plain name (a
// notification URL with its login).
const secretAttr = "data-secret"

// untouchedTypes are inputs the form keeps as rendered.
var untouchedTypes = map[string]bool{
	"password": true, "hidden": true, "file": true, "submit": true, "button": true, "reset": true, "image": true,
}

// stateField tells whether a hidden field records the state a form was
// rendered from (a board version, "was_area") rather than which row it
// is: a stale form (409) still matches its fresh copy and keeps the
// fresh value, so sending it again works.
func stateField(name string) bool {
	return name == "version" || strings.HasSuffix(name, "_version") || strings.HasPrefix(name, "was_")
}

// isSecret tells whether a field name names a secret.
func isSecret(name string) bool {
	parts := strings.FieldsFunc(strings.ToLower(name), func(r rune) bool { return r == '_' || r == '-' || r == '.' })
	return slices.ContainsFunc(parts, func(p string) bool { return secretParts[p] })
}

// keptValues are the sent values a refused form shows again: the body
// fields (the query of a get form) without secrets.
func keptValues(r *http.Request) url.Values {
	sent := r.URL.Query()
	if r.Method == http.MethodPost {
		if r.PostForm == nil {
			_ = r.ParseForm() // a bad body keeps nothing
		}
		sent = r.PostForm
	}
	out := url.Values{}
	for name, vs := range sent {
		if !isSecret(name) {
			out[name] = vs
		}
	}
	return out
}

// keepInputKey asks a page answered with 200 to refill the posted form
// all the same: an htmx answer to a refused form (htmx swaps no error).
const keepInputKey = "KeepInput"

// refusedPost is the request whose form a page shows again: a POST, or
// a GET with a query, answered with an error status (or marked with
// keepInputKey).
func refusedPost(ctx Ctx, status int, data map[string]any) *http.Request {
	if keep, _ := data[keepInputKey].(bool); keep && ctx.req != nil && ctx.req.Method == http.MethodPost {
		return ctx.req
	}
	if ctx.req == nil || status < http.StatusBadRequest {
		return nil
	}
	if ctx.req.Method == http.MethodPost || (ctx.req.Method == http.MethodGet && ctx.req.URL.RawQuery != "") {
		return ctx.req
	}
	return nil
}

// keepInput writes page with the posted form's fields refilled: the form
// whose action is the posted path and whose hidden fields match what was
// posted (rows of one list post to the same path with their own id).
func keepInput(w io.Writer, page []byte, r *http.Request) error {
	k := keeper{posted: keptValues(r), path: r.URL.Path, method: r.Method}
	z := html.NewTokenizer(bytes.NewReader(page))
	var form []part
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if z.Err() != io.EOF {
				return z.Err()
			}
			return k.flush(w, form, false)
		}
		p := part{raw: slices.Clone(z.Raw()), tt: tt}
		p.tok = z.Token()

		switch {
		case tt == html.StartTagToken && p.tok.DataAtom == atom.Form:
			if err := k.flush(w, form, false); err != nil {
				return err
			}
			form = []part{p}
		case form != nil && tt == html.EndTagToken && p.tok.DataAtom == atom.Form:
			if err := k.flush(w, append(form, p), k.matches(form)); err != nil {
				return err
			}
			form = nil
		case form != nil:
			form = append(form, p)
		default:
			if _, err := w.Write(p.raw); err != nil {
				return err
			}
		}
	}
}

// part is one token of a page: as rendered, and parsed.
type part struct {
	raw []byte
	tt  html.TokenType
	tok html.Token
}

// keeper refills one sent form.
type keeper struct {
	posted url.Values
	path   string
	method string
}

// matches tells whether form (its tokens up to </form>) is the one posted.
func (k keeper) matches(form []part) bool {
	method := attr(form[0].tok, "method")
	if method == "" {
		method = http.MethodGet
	}
	if !strings.EqualFold(method, k.method) {
		return false
	}
	action, err := url.Parse(attr(form[0].tok, "action"))
	if err != nil || (action.Path != "" && action.Path != k.path) {
		return false
	}
	for _, p := range form {
		if p.tok.DataAtom != atom.Input || attr(p.tok, "type") != "hidden" {
			continue
		}
		name := attr(p.tok, "name")
		if stateField(name) {
			continue
		}
		if sent, ok := k.posted[name]; ok && !slices.Contains(sent, attr(p.tok, "value")) {
			return false
		}
	}
	return true
}

// flush writes a form's tokens, refilled when refill is set.
func (k keeper) flush(w io.Writer, form []part, refill bool) error {
	used := map[string]int{} // values taken per name: rows of one name
	var selectName, areaValue string
	inArea := false
	for _, p := range form {
		out := p.raw
		switch {
		case !refill:
		case p.tt == html.EndTagToken && p.tok.DataAtom == atom.Select:
			selectName = ""
		case p.tt == html.EndTagToken && p.tok.DataAtom == atom.Textarea && inArea:
			inArea = false
			out = append([]byte(html.EscapeString(areaValue)), p.raw...)
		case inArea:
			continue // the rendered text, replaced at </textarea>
		case p.tt != html.StartTagToken && p.tt != html.SelfClosingTagToken:
		case p.tok.DataAtom == atom.Input:
			out = k.input(p, used)
		case p.tok.DataAtom == atom.Select:
			selectName = k.field(p.tok)
		case p.tok.DataAtom == atom.Option && selectName != "" && hasAttr(p.tok, "value"):
			out = setFlag(p.tok, "selected", slices.Contains(k.posted[selectName], attr(p.tok, "value")))
		case p.tok.DataAtom == atom.Textarea:
			if name := k.field(p.tok); name != "" {
				areaValue, inArea = k.next(name, used), true
			}
		}
		if _, err := w.Write(out); err != nil {
			return err
		}
	}
	return nil
}

// field is the name of a field to refill, "" for one that stays.
func (k keeper) field(tok html.Token) string {
	name := attr(tok, "name")
	if name == "" || isSecret(name) || hasAttr(tok, secretAttr) || hasAttr(tok, "disabled") {
		return ""
	}
	// An unchecked box sends nothing, other fields always send.
	if _, sent := k.posted[name]; !sent && tok.DataAtom != atom.Input {
		return ""
	}
	return name
}

// next takes the next posted value of name.
func (k keeper) next(name string, used map[string]int) string {
	i := used[name]
	used[name]++
	if i < len(k.posted[name]) {
		return k.posted[name][i]
	}
	return ""
}

// input refills one input: a box is checked when its value was posted,
// a text field gets its posted value.
func (k keeper) input(p part, used map[string]int) []byte {
	kind := strings.ToLower(attr(p.tok, "type"))
	name := k.field(p.tok)
	if name == "" || untouchedTypes[kind] {
		return p.raw
	}
	if kind == "checkbox" || kind == "radio" {
		value := attr(p.tok, "value")
		if !hasAttr(p.tok, "value") {
			value = "on"
		}
		return setFlag(p.tok, "checked", slices.Contains(k.posted[name], value))
	}
	if _, sent := k.posted[name]; !sent {
		return p.raw
	}
	return setAttr(p.tok, "value", k.next(name, used))
}

func attr(tok html.Token, name string) string {
	for _, a := range tok.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

func hasAttr(tok html.Token, name string) bool {
	return slices.ContainsFunc(tok.Attr, func(a html.Attribute) bool { return a.Key == name })
}

// setAttr renders tok with name set to value, in its place if it has one.
func setAttr(tok html.Token, name, value string) []byte {
	tok.Attr = slices.Clone(tok.Attr)
	i := slices.IndexFunc(tok.Attr, func(a html.Attribute) bool { return a.Key == name })
	if i < 0 {
		tok.Attr = append(tok.Attr, html.Attribute{Key: name, Val: value})
	} else {
		tok.Attr[i].Val = value
	}
	return []byte(tok.String())
}

// setFlag renders tok with a boolean attribute on or off.
func setFlag(tok html.Token, name string, on bool) []byte {
	tok.Attr = slices.DeleteFunc(slices.Clone(tok.Attr), func(a html.Attribute) bool { return a.Key == name })
	if on {
		tok.Attr = append(tok.Attr, html.Attribute{Key: name})
	}
	return []byte(tok.String())
}

// flashCookie carries a success message across a redirect; flashPrefix
// keeps it to the catalog's flash texts.
const (
	flashCookie   = "andon_flash"
	flashPrefix   = "flash."
	flashMaxAgeS  = 60
	flashSaved    = "flash.saved"
	flashDone     = "flash.done"
	flashSetup    = "flash.setup_done"
	flashReset    = "flash.password_reset"
	flashPassword = "flash.password_changed"
	flashRegister = "flash.registered"

	flashRegisterNoMail = "flash.registered_no_mail"
)

// flash sets the message the next page shows once (key: a flash.* text).
func (d Deps) flash(w http.ResponseWriter, key string) {
	http.SetCookie(w, &http.Cookie{
		Name: flashCookie, Value: key, Path: "/", MaxAge: flashMaxAgeS, HttpOnly: true,
		Secure: d.Settings.SecureCookies(), SameSite: http.SameSiteLaxMode,
	})
}

// takeFlash clears the message once a page shows it.
func (d Deps) takeFlash(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: flashCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: d.Settings.SecureCookies(), SameSite: http.SameSiteLaxMode,
	})
}

// flashOf is the message a request carries, "" for none or a foreign value.
func flashOf(r *http.Request) string {
	c, err := r.Cookie(flashCookie)
	if err != nil || !strings.HasPrefix(c.Value, flashPrefix) || !i18n.Has(c.Value) {
		return ""
	}
	return c.Value
}
