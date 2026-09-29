package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"andon/internal/sources"
)

// TestIconMustBeAnImage: an icon URL ending in .svg or a server claiming
// image/png does not make a JSON answer an icon; otherwise any internal
// response could be read back through /icons.
func TestIconMustBeAnImage(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	cases := []struct {
		path, contentType string
		body              []byte
		ok                bool
	}{
		{"/secret?x=.svg", "application/json", []byte(`{"token":"s3cret"}`), false},
		{"/secret.svg", "image/svg+xml", []byte(`{"token":"s3cret"}`), false},
		{"/a.png", "image/png", []byte(`{"token":"s3cret"}`), false},
		{"/a.png", "application/octet-stream", png, true},
		{"/a.svg", "image/svg+xml", []byte(`<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"></svg>`), true},
	}
	for _, c := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", c.contentType)
			w.Write(c.body)
		}))
		_, err := sources.FetchIcon(context.Background(), srv.URL+c.path, "")
		srv.Close()
		if (err == nil) != c.ok {
			t.Errorf("%s (%s): err %v, want ok=%v", c.path, c.contentType, err, c.ok)
		}
	}
}
