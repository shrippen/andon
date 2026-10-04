package util

// Widget config secrets. Status-check headers, API keys and private
// calendar links never rest in plain JSON:
//
//	form {api_key: k} ──Seal──► config {api_key_enc: base64(AES-GCM)}
//	config ──Open──► {api_key: k} (in memory, for the fetch only)
//	config ──Strip──► export without *_enc

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"reflect"
	"strings"

	"andon/internal/crypto"
)

const (
	encSuffix = "_enc"

	// SecretClear as a secret's value removes the stored one.
	SecretClear = "-"
)

// secretKeys are the config keys holding secrets.
var secretKeys = []string{"headers", "api_key", "ical_url", "ical_url_2", "ical_url_3", "ical_pass", "ical_pass_2", "ical_pass_3"}

// boundSecrets: a password kept only while the secret address it logs in
// to stays on the same host.
var boundSecrets = map[string]string{"ical_pass": "ical_url", "ical_pass_2": "ical_url_2", "ical_pass_3": "ical_url_3"}

func copyConfig(config map[string]any) map[string]any {
	out := make(map[string]any, len(config))
	for k, v := range config {
		out[k] = v
	}
	return out
}

// empty: "", nil or an empty map/list, i.e. "nothing typed".
func empty(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String, reflect.Map, reflect.Slice:
		return rv.Len() == 0
	}
	return false
}

// SealSecrets encrypts every secret in config. An empty value keeps the
// secret of prev, SecretClear drops it. Nothing is kept when a URL now
// points at another host: its headers were meant for the old one.
func SealSecrets(config, prev map[string]any) (map[string]any, error) {
	out := copyConfig(config)
	if !sameHosts(config, prev) {
		prev = nil
	}
	opened := OpenSecrets(prev)
	for _, key := range secretKeys {
		raw := out[key]
		delete(out, key)
		delete(out, key+encSuffix)

		if s, _ := raw.(string); s == SecretClear {
			continue
		}
		if empty(raw) && movedFeed(config, opened, key) {
			continue
		}
		if empty(raw) {
			if old, ok := prev[key+encSuffix]; ok {
				out[key+encSuffix] = old
			}
			continue
		}

		plain, err := json.Marshal(raw)
		if err != nil {
			return nil, err
		}
		blob, err := crypto.Encrypt(string(plain), crypto.PurposeCredential, nil)
		if err != nil {
			return nil, err
		}
		out[key+encSuffix] = base64.StdEncoding.EncodeToString(blob)
	}
	return out, nil
}

// movedFeed: key is bound to an address that config sets to another host.
func movedFeed(config, prev map[string]any, key string) bool {
	feed, bound := boundSecrets[key]
	if !bound || empty(config[feed]) {
		return false
	}
	if s, _ := config[feed].(string); s == SecretClear {
		return true
	}

	return hostOf(config[feed]) != hostOf(prev[feed])
}

// urlKeys are the config keys a tile's secrets are sent to.
var urlKeys = []string{"url", "status_url"}

// sameHosts: every URL config sets has the scheme and host it had in
// prev.
func sameHosts(config, prev map[string]any) bool {
	for _, key := range urlKeys {
		if _, set := config[key]; !set {
			continue
		}
		if hostOf(config[key]) != hostOf(prev[key]) {
			return false
		}
	}
	return true
}

// hostOf: "https://api.lan/x" → "https://api.lan"; "" for no URL.
func hostOf(v any) string {
	s, _ := v.(string)
	u, err := url.Parse(strings.TrimSpace(s))
	if err != nil || s == "" {
		return ""
	}
	return strings.ToLower(u.Scheme + "://" + u.Host)
}

// OpenSecrets returns config with its secrets decrypted; a secret that
// fails to open is left out, so the fetch runs without it.
func OpenSecrets(config map[string]any) map[string]any {
	out := config
	for _, key := range secretKeys {
		enc, _ := config[key+encSuffix].(string)
		if enc == "" {
			continue
		}
		blob, err := base64.StdEncoding.DecodeString(enc)
		if err != nil {
			continue
		}
		plain, err := crypto.Decrypt(blob, crypto.PurposeCredential)
		if err != nil {
			continue
		}
		var value any
		if json.Unmarshal([]byte(plain), &value) != nil {
			continue
		}
		out = withValue(out, config, key, value)
	}
	return out
}

// withValue sets key on a copy, copying only once.
func withValue(out, orig map[string]any, key string, value any) map[string]any {
	if reflect.ValueOf(out).Pointer() == reflect.ValueOf(orig).Pointer() {
		out = copyConfig(orig)
	}
	out[key] = value
	return out
}

// StripSecrets drops encrypted values: they are bound to this instance's
// key and must not leave it.
func StripSecrets(config map[string]any) map[string]any {
	out := config
	for _, key := range secretKeys {
		if _, ok := config[key+encSuffix]; !ok {
			continue
		}
		if reflect.ValueOf(out).Pointer() == reflect.ValueOf(config).Pointer() {
			out = copyConfig(config)
		}
		delete(out, key+encSuffix)
	}
	return out
}
