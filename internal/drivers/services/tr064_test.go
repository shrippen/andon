package services

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The web interface's URL reaches TR-064 on its own port: the URL a user
// copies from the browser answered 404 before.
func TestTR064Base(t *testing.T) {
	for url, want := range map[string]string{
		"http://fritz.box":       "http://fritz.box:49000",
		"https://fritz.box/":     "https://fritz.box:49443",
		"http://fritz.box:8000":  "http://fritz.box:8000",
		"https://192.168.178.1":  "https://192.168.178.1:49443",
		"https://fritz.box:4443": "https://fritz.box:4443",
	} {
		if got := (TR064{URL: url}).base(); got != want {
			t.Errorf("%s → %s, want %s", url, got, want)
		}
	}
}

// Arguments go into the action; a refusal names its UPnP code; a list
// the box names is read from its path on the TR-064 address.
func TestTR064ArgsAndLists(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/meshlist.lua" {
			w.Write([]byte(r.URL.Query().Get("sid")))
			return
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "<NewIndex>3</NewIndex>") {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`<s:Envelope><s:Body><s:Fault><detail><UPnPError><errorCode>713</errorCode></UPnPError></detail></s:Fault></s:Body></s:Envelope>`))
			return
		}
		w.Write([]byte(`<r><NewAIN>12345 0000001</NewAIN></r>`))
	}))
	defer srv.Close()
	box := TR064{URL: srv.URL}
	ctx := context.Background()

	got, err := box.Call(ctx, "x_homeauto", "X_AVM-DE_Homeauto:1", "GetGenericDeviceInfos", Arg{"NewIndex", "3"})
	if err != nil || got["NewAIN"] != "12345 0000001" {
		t.Fatalf("call %v %v", got, err)
	}
	_, err = box.Call(ctx, "x_homeauto", "X_AVM-DE_Homeauto:1", "GetGenericDeviceInfos", Arg{"NewIndex", "4"})
	var refused UPnPError
	if !errors.As(err, &refused) || refused.Code != "713" {
		t.Fatalf("refusal %v", err)
	}
	// The box names its LAN address; only path and query count.
	raw, err := box.Fetch(ctx, "http://192.168.178.1:49000/meshlist.lua?sid=abc")
	if err != nil || string(raw) != "abc" {
		t.Fatalf("fetch %q %v", raw, err)
	}
}
