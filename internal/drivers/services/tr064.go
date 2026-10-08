package services

// FRITZ!Box TR-064: SOAP actions over HTTP(S) with digest login.
//
//	POST /upnp/control/<control>   SOAPACTION: "urn:dslforum-org:service:<service>#<action>"
//	← 401 WWW-Authenticate: Digest realm="F!Box SOAP-Auth", nonce="…", qop="auth"
//	→ Authorization: Digest username, realm, nonce, uri, response=MD5(HA1:nonce:nc:cnonce:qop:HA2) …
//	← <s:Envelope><s:Body><u:GetInfoResponse><NewUptime>2400</NewUptime>…
//	← 500 <UPnPError><errorCode>713</errorCode>…   (e.g. an index past the end)
//
// Some actions answer with a path to a list (hosts, mesh, calls); Fetch
// reads it with the session id the box put into it.

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"

	"andon/internal/drivers/httpclient"
)

// TR064 calls actions of one FRITZ!Box.
type TR064 struct {
	URL, User, Password string
	Mode                httpclient.TLS
}

// TR-064 has its own ports; the web interface (80, 443) answers 404.
const (
	tr064Port    = "49000"
	tr064TLSPort = "49443"
)

// base is the box's TR-064 address: the URL as given when it names a
// port, else the TR-064 port of its scheme.
//
//	http://fritz.box       → http://fritz.box:49000
//	https://fritz.box      → https://fritz.box:49443
//	http://fritz.box:8000  → as given (a forwarded port)
func (t TR064) base() string {
	raw := strings.TrimRight(t.URL, "/")
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Port() != "" {
		return raw
	}
	port := tr064Port
	if u.Scheme == "https" {
		port = tr064TLSPort
	}
	u.Host = net.JoinHostPort(u.Hostname(), port)
	return u.String()
}

const tr064Envelope = `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">` +
	`<s:Body><u:%s xmlns:u="urn:dslforum-org:service:%s">%s</u:%s></s:Body></s:Envelope>`

// Arg is one in-argument of an action: Arg{"NewIndex", "0"}.
type Arg struct{ Name, Value string }

// Call runs an action and returns its out-arguments:
// Call(ctx, "wandslifconfig1", "WANDSLInterfaceConfig:1", "GetInfo") → {"NewUpstreamCurrRate": "40000", …}
func (t TR064) Call(ctx context.Context, control, service, action string, args ...Arg) (map[string]string, error) {
	target := t.base() + "/upnp/control/" + control
	var in strings.Builder
	for _, a := range args {
		in.WriteString("<" + a.Name + ">")
		_ = xml.EscapeText(&in, []byte(a.Value))
		in.WriteString("</" + a.Name + ">")
	}
	body := []byte(fmt.Sprintf(tr064Envelope, action, service, in.String(), action))
	headers := map[string]string{"Content-Type": `text/xml; charset="utf-8"`, "SOAPACTION": `"urn:dslforum-org:service:` + service + "#" + action + `"`}

	resp, err := httpclient.Request(ctx, http.MethodPost, target, httpclient.Options{Headers: headers, Body: body, SkipVerify: t.Mode == httpclient.TLSSkip})
	if err != nil {
		return nil, ApiError{err.Error()}
	}
	if resp.StatusCode == http.StatusUnauthorized {
		challenge := resp.Header.Get("WWW-Authenticate")
		resp.Body.Close()
		headers["Authorization"] = digestAuth(challenge, t.User, t.Password, http.MethodPost, "/upnp/control/"+control)
		resp, err = httpclient.Request(ctx, http.MethodPost, target, httpclient.Options{Headers: headers, Body: body, SkipVerify: t.Mode == httpclient.TLSSkip})
		if err != nil {
			return nil, ApiError{err.Error()}
		}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, httpclient.MaxBody))
	if err != nil {
		return nil, ApiError{err.Error()}
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, upnpError(resp.StatusCode, soapValues(raw)["errorCode"])
	}
	return soapValues(raw), nil
}

// UPnPError is the box refusing an action, with its UPnP code:
// 713 an index past the end, 820 not supported, 606 not allowed.
type UPnPError struct {
	Status int
	Code   string
}

func (e UPnPError) Error() string { return fmt.Sprintf("HTTP %d (UPnP %s)", e.Status, e.Code) }

func upnpError(status int, code string) error {
	if code == "" {
		return ApiError{fmt.Sprintf("HTTP %d", status)}
	}
	return UPnPError{Status: status, Code: code}
}

// Fetch reads a list the box named in an answer ("/devicehostlist.lua?sid=…",
// or a whole URL for the call list). Only path and query are taken: the
// box names its own LAN address, which need not be the one Andon reaches.
func (t TR064) Fetch(ctx context.Context, ref string) ([]byte, error) {
	u, err := url.Parse(ref)
	if err != nil {
		return nil, ApiError{"bad list path"}
	}
	target := t.base() + "/" + strings.TrimLeft(u.Path, "/")
	if u.RawQuery != "" {
		target += "?" + u.RawQuery
	}

	resp, err := httpclient.Request(ctx, http.MethodGet, target, httpclient.Options{SkipVerify: t.Mode == httpclient.TLSSkip})
	if err != nil {
		return nil, ApiError{err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, ApiError{fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, httpclient.MaxBody))
	if err != nil {
		return nil, ApiError{err.Error()}
	}
	return raw, nil
}

// soapValues reads the leaf elements of a SOAP answer by local name.
func soapValues(raw []byte) map[string]string {
	out := map[string]string{}
	dec := xml.NewDecoder(bytes.NewReader(raw))
	var name string
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		switch v := tok.(type) {
		case xml.StartElement:
			name = v.Name.Local
		case xml.CharData:
			if text := strings.TrimSpace(string(v)); text != "" && name != "" {
				out[name] = text
			}
		case xml.EndElement:
			name = ""
		}
	}
}

// digestAuth answers an RFC 7616 MD5 challenge with qop=auth.
func digestAuth(challenge, user, password, method, uri string) string {
	params := map[string]string{}
	for _, part := range strings.Split(strings.TrimPrefix(challenge, "Digest "), ",") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok {
			params[k] = strings.Trim(v, `"`)
		}
	}
	md5hex := func(s string) string { sum := md5.Sum([]byte(s)); return hex.EncodeToString(sum[:]) }
	nonceBytes := make([]byte, 8)
	_, _ = rand.Read(nonceBytes)
	cnonce, nc := hex.EncodeToString(nonceBytes), "00000001"
	ha1 := md5hex(user + ":" + params["realm"] + ":" + password)
	ha2 := md5hex(method + ":" + uri)
	response := md5hex(ha1 + ":" + params["nonce"] + ":" + nc + ":" + cnonce + ":auth:" + ha2)
	return fmt.Sprintf(`Digest username="%s", realm="%s", nonce="%s", uri="%s", qop=auth, nc=%s, cnonce="%s", response="%s"`,
		user, params["realm"], params["nonce"], uri, nc, cnonce, response)
}
