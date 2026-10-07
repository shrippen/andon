package services

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"andon/internal/drivers/httpclient"
)

// TestUrBackupLogin: the password hash follows UrBackup's own wrapper
// (reference computed with its Python code), the session rides along.
func TestUrBackupLogin(t *testing.T) {
	const want = "3ba38ef56cf2e6ab8a79c78708fec015" // salt s4lt, password geheim, rnd r4nd, 10000 rounds
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.URL.Query().Get("a") {
		case "salt":
			w.Write([]byte(`{"ses":"S1","salt":"s4lt","rnd":"r4nd","pbkdf2_rounds":10000}`))
		case "login":
			json.NewEncoder(w).Encode(map[string]any{"success": r.Form.Get("password") == want && r.Form.Get("ses") == "S1"})
		case "status":
			if r.Form.Get("ses") != "S1" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			w.Write([]byte(`{"status":[{"name":"pc"}]}`))
		}
	}))
	defer srv.Close()

	raw, err := UrBackupApi{URL: srv.URL + "/x", User: "admin", Password: "geheim", Mode: httpclient.TLSVerify}.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if list := raw.(map[string]any)["status"].([]any); len(list) != 1 {
		t.Fatalf("status %v", raw)
	}
	if _, err := (UrBackupApi{URL: srv.URL, User: "admin", Password: "falsch"}).Status(context.Background()); err != ErrLogin {
		t.Fatalf("wrong password: %v", err)
	}
}

// TestDuplicatiLogin: the token from auth/login opens the backups list.
func TestDuplicatiLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/auth/login":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if body["Password"] != "pw" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Write([]byte(`{"AccessToken":"T"}`))
		case "/api/v1/backups":
			if r.Header.Get("Authorization") != "Bearer T" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Write([]byte(`[{"Backup":{"ID":"1","Name":"Docs"}}]`))
		}
	}))
	defer srv.Close()
	raw, err := DuplicatiApi{URL: srv.URL, Password: "pw", Mode: httpclient.TLSVerify}.Backups(context.Background())
	if err != nil || len(raw.([]any)) != 1 {
		t.Fatalf("%v %v", raw, err)
	}
}

// TestApcupsdStatus: the NIS frames come back as key/value lines.
func TestApcupsdStatus(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		head := make([]byte, 8)
		io.ReadFull(c, head) // 00 06 "status"
		for _, line := range []string{"STATUS   : ONLINE\n", "BCHARGE  : 92.0 Percent\n"} {
			c.Write(append(binary.BigEndian.AppendUint16(nil, uint16(len(line))), line...))
		}
		c.Write([]byte{0, 0})
	}()
	got, err := ApcupsdStatus(context.Background(), l.Addr().String())
	if err != nil || got["STATUS"] != "ONLINE" || got["BCHARGE"] != "92.0 Percent" {
		t.Fatalf("%v %v", got, err)
	}
}
