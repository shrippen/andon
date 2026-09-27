package web

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestStaticIsCompressed: CSS and JS go out gzipped to browsers that
// accept it, byte-identical after decompression.
func TestStaticIsCompressed(t *testing.T) {
	mux := http.NewServeMux()
	Deps{}.RegisterStaticRoutes(mux)
	want, err := staticFiles.ReadFile("static/andon.css")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, asset("andon.css"), nil)
	req.Header.Set("Accept-Encoding", "br, gzip")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "gzip" || rec.Header().Get("Vary") != "Accept-Encoding" {
		t.Fatalf("headers: %v", rec.Header())
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(zr)
	if string(got) != string(want) {
		t.Fatal("decompressed body differs")
	}

	plain := httptest.NewRecorder()
	mux.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, asset("andon.css"), nil))
	if plain.Header().Get("Content-Encoding") != "" || plain.Body.String() != string(want) {
		t.Fatal("client without gzip must get the plain file")
	}
}
