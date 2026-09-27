package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"andon/internal/drivers/httpclient"
)

func TestAllowlistBlocksPrivateUnlessListed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	t.Cleanup(func() { httpclient.SetGuard(nil) })

	call := func() error {
		resp, err := httpclient.Request(context.Background(), http.MethodGet, srv.URL, httpclient.Options{})
		if err == nil {
			resp.Body.Close()
		}
		return err
	}

	if err := ApplyNetwork(NetworkPolicy{Mode: NetAllowlist, Public: true}); err != nil {
		t.Fatal(err)
	}
	if _, denied := call().(httpclient.EgressDenied); !denied {
		t.Fatal("expected loopback denied under allowlist")
	}

	if err := ApplyNetwork(NetworkPolicy{Mode: NetAllowlist, Networks: []string{"127.0.0.0/8"}}); err != nil {
		t.Fatal(err)
	}
	if err := call(); err != nil {
		t.Fatalf("expected listed network allowed: %v", err)
	}

	if err := ApplyNetwork(NetworkPolicy{Mode: NetOpen, Networks: []string{"127.0.0.0/8"}}); err != nil {
		t.Fatal(err)
	}
	if err := call(); err != nil {
		t.Fatalf("expected listed loopback allowed in open mode: %v", err)
	}
}

// TestOpenModeBlocksLocalTargets: without admin settings, users still
// cannot reach Andon itself (loopback) or cloud metadata (link-local).
func TestOpenModeBlocksLocalTargets(t *testing.T) {
	t.Cleanup(func() { httpclient.SetGuard(nil) })
	if err := ApplyNetwork(NetworkPolicy{Mode: NetOpen, Public: true}); err != nil {
		t.Fatal(err)
	}

	for _, target := range []string{"http://127.0.0.1:1/", "http://169.254.169.254/latest/meta-data/", "http://[::1]:1/", "http://0.0.0.0:1/"} {
		_, err := httpclient.Request(context.Background(), http.MethodGet, target, httpclient.Options{})
		if _, denied := err.(httpclient.EgressDenied); !denied {
			t.Errorf("%s: expected EgressDenied, got %v", target, err)
		}
	}
}
