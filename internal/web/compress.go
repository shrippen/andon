package web

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

// Compression of dynamic answers: a 240-tile board is half a megabyte of
// HTML, ~40 kB gzipped. Static files have their own (gzipStatic).
//
//	handler ─► gzipAnswer ─(HTML/JSON, gzip accepted)─► gzip.Writer ─► client
//	                      └─(anything else)─────────────────────────► client
//
// The CSRF token in pages is masked per answer (Page), so compression
// does not leak it (BREACH).

// compressTypes are the answer types worth compressing.
var compressTypes = []string{"text/html", "application/json"}

// gzipWriters reuses writers: each allocates about a megabyte.
var gzipWriters = sync.Pool{New: func() any {
	zw, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
	return zw
}}

// compressAnswers gzips the answers of next for clients that accept it.
func compressAnswers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		answer := &gzipAnswer{ResponseWriter: w}
		defer answer.close()
		next.ServeHTTP(answer, r)
	})
}

// gzipAnswer decides on its first header or write whether to compress.
type gzipAnswer struct {
	http.ResponseWriter
	zw      *gzip.Writer
	decided bool
}

func (g *gzipAnswer) WriteHeader(status int) {
	if !g.decided {
		g.decide(status)
	}
	g.ResponseWriter.WriteHeader(status)
}

func (g *gzipAnswer) Write(b []byte) (int, error) {
	if !g.decided {
		if g.Header().Get("Content-Type") == "" {
			g.Header().Set("Content-Type", http.DetectContentType(b))
		}
		g.WriteHeader(http.StatusOK)
	}
	if g.zw == nil {
		return g.ResponseWriter.Write(b)
	}
	return g.zw.Write(b)
}

// decide compresses bodies of a compressible type not encoded already.
func (g *gzipAnswer) decide(status int) {
	g.decided = true
	h := g.Header()
	if status == http.StatusNoContent || status == http.StatusNotModified || h.Get("Content-Encoding") != "" || !compressType(h.Get("Content-Type")) {
		return
	}
	h.Del("Content-Length")
	h.Set("Content-Encoding", "gzip")
	h.Add("Vary", "Accept-Encoding")
	g.zw = gzipWriters.Get().(*gzip.Writer)
	g.zw.Reset(g.ResponseWriter)
}

func (g *gzipAnswer) close() {
	if g.zw == nil {
		return
	}
	_ = g.zw.Close()
	gzipWriters.Put(g.zw)
}

// compressType reports an answer type worth compressing.
func compressType(contentType string) bool {
	for _, t := range compressTypes {
		if strings.HasPrefix(contentType, t) {
			return true
		}
	}
	return false
}
