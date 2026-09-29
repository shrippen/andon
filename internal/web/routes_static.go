package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"sync"
)

//go:embed static
var staticFiles embed.FS

// RegisterStaticRoutes serves vendored assets (htmx, ...) so board pages
// can lazy-load widget fragments without a CDN dependency.
func (d Deps) RegisterStaticRoutes(mux *http.ServeMux) {
	mux.Handle("GET /static/", cacheStatic(tagStatic(gzipStatic(http.FileServerFS(staticFiles)))))
	mux.HandleFunc("GET /sw.js", handleWorker)
}

// handleWorker serves the service worker from the root so its scope
// covers every page (offline view).
func handleWorker(w http.ResponseWriter, r *http.Request) {
	body, err := staticFiles.ReadFile("static/sw.js")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(body)
}

// assetVersion is a hash over all static files: it changes with every
// release that touches them, e.g. /static/andon.css?v=3f2a9c01d4.
var assetVersion = sync.OnceValue(func() string {
	h := sha256.New()
	_ = fs.WalkDir(staticFiles, "static", func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		body, err := staticFiles.ReadFile(path)
		if err != nil {
			return err
		}
		h.Write([]byte(path))
		h.Write(body)
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))[:assetVersionLen]
})

const assetVersionLen = 10

// asset is the template func for a static file's versioned URL.
func asset(name string) string {
	return "/static/" + name + "?v=" + assetVersion()
}

// cacheStatic lets browsers keep versioned files for good, so a page change
// reads CSS and JS from the cache; plain URLs revalidate every time.
func cacheStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("v") {
			w.Header().Set("Cache-Control", cacheForever)
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		next.ServeHTTP(w, r)
	})
}

// compressible are the text assets worth gzipping; fonts (WOFF2) and
// images are compressed already.
var compressible = map[string]string{
	".css": "text/css; charset=utf-8",
	".js":  "text/javascript; charset=utf-8",
	".svg": "image/svg+xml",
}

// tags holds each file's ETag once; the files are embedded and never
// change while the process runs.
var tags sync.Map // path → string

// tagStatic gives every file an ETag. Embedded files have no modification
// time, so an unversioned URL (fonts linked from andon.css) was sent in
// full on every page load; now it revalidates with a 304.
func tagStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tag, err := tagOf(strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("ETag", tag)
		next.ServeHTTP(w, r)
	})
}

// tagLen is how many hex digits of the SHA-256 name a file's version.
const tagLen = 16

func tagOf(name string) (string, error) {
	if cached, ok := tags.Load(name); ok {
		return cached.(string), nil
	}
	raw, err := staticFiles.ReadFile(name)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	tag := `"` + hex.EncodeToString(sum[:])[:tagLen] + `"`
	tags.Store(name, tag)
	return tag, nil
}

// gzipped holds each compressed asset once; the files are embedded and
// never change while the process runs.
var gzipped sync.Map // path → []byte

// gzipStatic answers CSS, JS and SVG gzipped when the browser accepts it
// (andon.css: about 100 kB → 20 kB), compressing each file only once.
func gzipStatic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		kind, ok := compressible[path.Ext(r.URL.Path)]
		if !ok || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		body, err := gzipOf(strings.TrimPrefix(r.URL.Path, "/"))
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		h := w.Header()
		h.Set("Vary", "Accept-Encoding")

		// The gzipped bytes differ from the file: their own tag.
		if tag := h.Get("ETag"); tag != "" {
			tag = strings.TrimSuffix(tag, `"`) + `-gz"`
			h.Set("ETag", tag)
			if r.Header.Get("If-None-Match") == tag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		h.Set("Content-Type", kind)
		h.Set("Content-Encoding", "gzip")
		w.Write(body)
	})
}

func gzipOf(name string) ([]byte, error) {
	if cached, ok := gzipped.Load(name); ok {
		return cached.([]byte), nil
	}
	raw, err := staticFiles.ReadFile(name)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.Write(raw)
	if err := zw.Close(); err != nil {
		return nil, err
	}
	gzipped.Store(name, buf.Bytes())
	return buf.Bytes(), nil
}
