package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"andon/internal/sources"
)

// TestPublicIPv6: IPv6 is looked up only when asked, and a host without
// it simply shows none.
func TestPublicIPv6(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ip":
			w.Write([]byte(`{"ip": "203.0.113.5"}`))
		case "/v6":
			w.Write([]byte(`{"ip": "2001:db8::5"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	defer sources.SetBases(srv.URL)()
	defer sources.SetPublicIPv6(srv.URL + "/v6")()

	out, err := sources.PublicIPSource.Fetch(context.Background(), sources.Ctx{Params: map[string]any{"v6": true}})
	if err != nil {
		t.Fatal(err)
	}
	if ip := out.(*sources.PublicIPResult); ip.IP != "203.0.113.5" || ip.IPv6 != "2001:db8::5" {
		t.Fatalf("ip: %+v", ip)
	}
	out, _ = sources.PublicIPSource.Fetch(context.Background(), sources.Ctx{})
	if ip := out.(*sources.PublicIPResult); ip.IPv6 != "" {
		t.Fatalf("v6 not asked: %+v", ip)
	}
	sources.SetPublicIPv6(srv.URL + "/none")
	out, err = sources.PublicIPSource.Fetch(context.Background(), sources.Ctx{Params: map[string]any{"v6": true}})
	if err != nil || out.(*sources.PublicIPResult).IPv6 != "" {
		t.Fatalf("no v6: %+v %v", out, err)
	}
}
