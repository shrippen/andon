package httpclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"andon/internal/drivers/httpclient"
)

// TestRedirectKeepsPost: a 301 turns a POST into a GET without body, so
// the login lands on a 404. The error names the target instead, e.g.
//
//	POST http://app.lan/api/login  --301-->  https://app.example/api/login
func TestRedirectKeepsPost(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer target.Close()
	bounce := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusMovedPermanently)
	}))
	defer bounce.Close()

	resp, err := httpclient.Request(context.Background(), http.MethodPost, bounce.URL+"/login", httpclient.Options{Body: []byte(`{}`)})
	if err == nil {
		resp.Body.Close()
		t.Fatalf("POST followed the 301 as GET: HTTP %d", resp.StatusCode)
	}
	if !strings.Contains(err.Error(), target.URL) {
		t.Fatalf("error should name the target %s: %v", target.URL, err)
	}

	// GET still follows.
	if _, _, err := httpclient.GetJSON(context.Background(), bounce.URL+"/x", httpclient.Options{}); err == nil || strings.Contains(err.Error(), "redirect") {
		t.Fatalf("GET should follow to the 404, got %v", err)
	}
}
