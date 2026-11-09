// Package dbtest holds fixtures for tests that open a database.
package dbtest

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ncruces/go-sqlite3"
)

// Key is a fixed database file key for tests (32 bytes).
var Key = []byte("0123456789abcdef0123456789abcdef")

// settle is how long Statements waits for connections in use.
const settle = 5 * time.Second

// Statements records the statements d runs from now on, e.g. to show a
// listing costs the same few queries for 1 or 40 rows. d keeps a single
// connection, so the record sees every statement; nested transactions
// would wait on it.
func Statements(t testing.TB, d *sql.DB) func() []string {
	t.Helper()
	// One connection, the traced one: wait for background work to hand
	// its connection back, else the pool may keep that one and close ours.
	for deadline := time.Now().Add(settle); d.Stats().InUse > 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("statements: %d connections stay in use", d.Stats().InUse)
		}
	}
	d.SetMaxOpenConns(1)
	d.SetMaxIdleConns(1)
	c, err := d.Conn(context.Background())
	if err != nil {
		t.Fatalf("statements: %v", err)
	}
	defer c.Close()

	var mu sync.Mutex
	var seen []string
	record := func(_ sqlite3.TraceEvent, _, sql any) error {
		mu.Lock()
		defer mu.Unlock()
		seen = append(seen, fmt.Sprint(sql))
		return nil
	}
	err = c.Raw(func(dc any) error {
		return dc.(interface{ Raw() *sqlite3.Conn }).Raw().Trace(sqlite3.TRACE_STMT, record)
	})
	if err != nil {
		t.Fatalf("statements: %v", err)
	}
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(seen)
	}
}
