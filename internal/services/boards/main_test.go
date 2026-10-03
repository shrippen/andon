package boards_test

import (
	"os"
	"testing"

	"andon/internal/services/icons"
)

// TestMain keeps icon lookups (favicons of test links) out of the source
// tree: without a directory they land in ./cache.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "andon-icons")
	if err != nil {
		panic(err)
	}
	icons.Init(dir)
	code := m.Run()
	icons.Wait()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}
