package db_test

import (
	"errors"
	"path/filepath"
	"testing"

	"andon/internal/db"
)

// TestLockKeepsSecondProcessOut: while one process holds the lock a
// second one is refused; after release it gets it.
func TestLockKeepsSecondProcessOut(t *testing.T) {
	path := filepath.Join(t.TempDir(), "andon.db")
	first, err := db.Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Lock(path); !errors.Is(err, db.ErrLocked) {
		t.Fatalf("second lock: %v, want ErrLocked", err)
	}
	first.Release()
	second, err := db.Lock(path)
	if err != nil {
		t.Fatalf("lock after release: %v", err)
	}
	second.Release()
}
