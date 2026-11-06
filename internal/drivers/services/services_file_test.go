package services

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"andon/internal/drivers/httpclient"
)

// A scan can be larger than an API answer: cut off at MaxBody a PDF no
// longer opens. Before the fix a 6 MiB file came back with 5 MiB, no error.
func TestPaperlessFileWhole(t *testing.T) {
	file := bytes.Repeat([]byte("x"), httpclient.MaxBody+httpclient.MaxBody/5)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Write(file)
	}))
	defer srv.Close()

	body, kind, err := PaperlessApi{URL: srv.URL, Token: "t", Verify: true}.File(context.Background(), "documents/1/download/")
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != len(file) || kind != "application/pdf" {
		t.Errorf("got %d bytes of %s, want %d", len(body), kind, len(file))
	}
}

// A file above the limit is an error, never a cut-off file.
func TestPaperlessFileTooLarge(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(make([]byte, maxFile+1))
	}))
	defer srv.Close()

	if body, _, err := (PaperlessApi{URL: srv.URL, Token: "t", Verify: true}).File(context.Background(), "documents/1/download/"); err == nil {
		t.Errorf("no error, %d bytes", len(body))
	}
}
