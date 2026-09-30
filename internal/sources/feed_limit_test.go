package sources

import (
	"io"
	"strings"
	"testing"
)

// endless never ends: a feed URL answering forever.
type endless struct{}

func (endless) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}

// TestFeedBodyLimited: an endless answer is cut off, not read into memory.
func TestFeedBodyLimited(t *testing.T) {
	if _, err := parseFeed(io.MultiReader(strings.NewReader("<rss>"), endless{})); err == nil {
		t.Fatal("endless feed parsed")
	}
}
