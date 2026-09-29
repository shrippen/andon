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

// fontPath is linked from andon.css without a version, so it is
// revalidated on every full page load.
const fontPath = "/static/vendor/kante/fonts/Rajdhani-600.ttf"

// TestStaticRevalidates: unversioned files carry an ETag and answer a
// matching If-None-Match with 304, gzipped or not; fonts are gzipped.
func TestStaticRevalidates(t *testing.T) {
	mux := http.NewServeMux()
	Deps{}.RegisterStaticRoutes(mux)

	for _, encoding := range []string{"", "gzip"} {
		req := httptest.NewRequest(http.MethodGet, fontPath, nil)
		req.Header.Set("Accept-Encoding", encoding)
		first := httptest.NewRecorder()
		mux.ServeHTTP(first, req)
		if first.Header().Get("Content-Encoding") != encoding {
			t.Fatalf("encoding %q: got %v", encoding, first.Header())
		}
		tag := first.Header().Get("ETag")
		if tag == "" {
			t.Fatalf("encoding %q: no ETag", encoding)
		}

		req.Header.Set("If-None-Match", tag)
		again := httptest.NewRecorder()
		mux.ServeHTTP(again, req)
		if again.Code != http.StatusNotModified || again.Body.Len() != 0 {
			t.Fatalf("encoding %q: revalidation got %d, %d bytes", encoding, again.Code, again.Body.Len())
		}
	}
}
