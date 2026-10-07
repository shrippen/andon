package services

// Logins of backup tools that need more than a header.
//
//	UrBackup   POST x?a=salt {username} → {ses, salt, rnd, pbkdf2_rounds}
//	           password = md5(rnd + hex(pbkdf2_sha256(md5(salt+password), salt, rounds)))
//	           POST x?a=login {username, password, ses} → {success}; then x?a=status {ses}
//	           (without a user: POST x?a=login → {success, session})
//	Duplicati  POST api/v1/auth/login {Password} → {AccessToken}; then Bearer

import (
	"context"
	"crypto/md5"
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"andon/internal/drivers/httpclient"
)

// ErrLogin: the service refused the login.
var ErrLogin = errors.New("services: login refused")

// PostForm sends form fields and decodes the JSON answer.
func (a KeyedApi) PostForm(ctx context.Context, path string, form url.Values) (any, error) {
	headers := map[string]string{"Content-Type": "application/x-www-form-urlencoded; charset=UTF-8", "Accept": "application/json"}
	for k, v := range a.Headers {
		headers[k] = v
	}
	resp, err := httpclient.Request(ctx, http.MethodPost, joinURL(a.URL, path), httpclient.Options{Headers: headers, Body: []byte(form.Encode()), SkipVerify: !a.Verify})
	if err != nil {
		return nil, ApiError{err.Error()}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return nil, ApiError{fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}
	var out any
	if err := json.NewDecoder(io.LimitReader(resp.Body, httpclient.MaxBody)).Decode(&out); err != nil {
		return nil, ApiError{"invalid JSON"}
	}
	return out, nil
}

// ── UrBackup ──

// UrBackupApi is the web interface's JSON API at <url>/x.
type UrBackupApi struct {
	URL, User, Password string
	Mode                httpclient.TLS
}

// Status logs in and returns the "status" answer (clients and their backups).
func (a UrBackupApi) Status(ctx context.Context) (any, error) {
	api := KeyedApi{URL: strings.TrimSuffix(strings.TrimRight(a.URL, "/"), "/x"), Verify: a.Mode == httpclient.TLSVerify}
	session, err := a.login(ctx, api)
	if err != nil {
		return nil, err
	}
	return api.PostForm(ctx, "x?a=status", url.Values{"ses": {session}})
}

func (a UrBackupApi) login(ctx context.Context, api KeyedApi) (string, error) {
	if a.User == "" {
		raw, err := api.PostForm(ctx, "x?a=login", url.Values{})
		if err != nil {
			return "", err
		}
		m, _ := raw.(map[string]any)
		if ok, _ := m["success"].(bool); !ok {
			return "", ErrLogin
		}
		session, _ := m["session"].(string)
		return session, nil
	}

	raw, err := api.PostForm(ctx, "x?a=salt", url.Values{"username": {a.User}})
	if err != nil {
		return "", err
	}
	salt, _ := raw.(map[string]any)
	session, _ := salt["ses"].(string)
	saltText, _ := salt["salt"].(string)
	rnd, _ := salt["rnd"].(string)
	if session == "" || saltText == "" {
		return "", ErrLogin
	}
	sum := md5.Sum([]byte(saltText + a.Password))
	hash := hex.EncodeToString(sum[:])
	if rounds := roundsOf(salt["pbkdf2_rounds"]); rounds > 0 {
		key, err := pbkdf2.Key(sha256.New, string(sum[:]), []byte(saltText), rounds, sha256.Size)
		if err != nil {
			return "", err
		}
		hash = hex.EncodeToString(key)
	}
	final := md5.Sum([]byte(rnd + hash))
	raw, err = api.PostForm(ctx, "x?a=login", url.Values{"username": {a.User}, "password": {hex.EncodeToString(final[:])}, "ses": {session}})
	if err != nil {
		return "", err
	}
	m, _ := raw.(map[string]any)
	if ok, _ := m["success"].(bool); !ok {
		return "", ErrLogin
	}
	return session, nil
}

// roundsOf reads pbkdf2_rounds, a number or a numeric string.
func roundsOf(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	}
	return 0
}

// ── Duplicati ──

// DuplicatiApi is Duplicati's REST API, logged in with the UI password.
type DuplicatiApi struct {
	URL, Password string
	Mode          httpclient.TLS
}

// Backups logs in and lists the backup jobs.
func (a DuplicatiApi) Backups(ctx context.Context) (any, error) {
	login := KeyedApi{URL: a.URL, Headers: map[string]string{}, Verify: a.Mode == httpclient.TLSVerify}
	raw, err := login.Post(ctx, "api/v1/auth/login", map[string]any{"Password": a.Password, "RememberMe": false})
	if err != nil {
		return nil, err
	}
	m, _ := raw.(map[string]any)
	token, _ := m["AccessToken"].(string)
	if token == "" {
		return nil, ErrLogin
	}
	return BearerApi(a.URL, token, a.Mode).Get(ctx, "api/v1/backups", nil)
}
