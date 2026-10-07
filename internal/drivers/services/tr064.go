package services

// FRITZ!Box TR-064: SOAP actions over HTTP(S) with digest login.
//
//	POST /upnp/control/<control>   SOAPACTION: "urn:dslforum-org:service:<service>#<action>"
//	← 401 WWW-Authenticate: Digest realm="F!Box SOAP-Auth", nonce="…", qop="auth"
//	→ Authorization: Digest username, realm, nonce, uri, response=MD5(HA1:nonce:nc:cnonce:qop:HA2) …
//	← <s:Envelope><s:Body><u:GetInfoResponse><NewUptime>2400</NewUptime>…

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"

	"andon/internal/drivers/httpclient"
)

// TR064 calls actions of one FRITZ!Box.
type TR064 struct {
	URL, User, Password string
	Mode                httpclient.TLS
}

const tr064Envelope = `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/">` +
	`<s:Body><u:%s xmlns:u="urn:dslforum-org:service:%s"/></s:Body></s:Envelope>`

// Call runs an action without arguments and returns its out-arguments:
// Call(ctx, "wandslifconfig1", "WANDSLInterfaceConfig:1", "GetInfo") → {"NewUpstreamCurrRate": "40000", …}
func (t TR064) Call(ctx context.Context, control, service, action string) (map[string]string, error) {
	target := strings.TrimRight(t.URL, "/") + "/upnp/control/" + control
	body := []byte(fmt.Sprintf(tr064Envelope, action, service))
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
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, ApiError{fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, httpclient.MaxBody))
	if err != nil {
		return nil, ApiError{err.Error()}
	}
	return soapValues(raw), nil
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
