package web_test

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// wrappedLoading is the loading placeholder inside a paragraph: a busy
// region dims its direct children except .loading, so the wrapper would
// dim the placeholder (Kante 1.47, aria-busy).
var wrappedLoading = regexp.MustCompile(`<p>[^<]*\{\{template "loading"`)

func TestLoadingNotWrapped(t *testing.T) {
	files, err := filepath.Glob("templates/*.html")
	if err != nil || len(files) == 0 {
		t.Fatalf("templates: %v", err)
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if wrappedLoading.Match(b) {
			t.Errorf("%s: loading placeholder inside <p>", f)
		}
	}
}
