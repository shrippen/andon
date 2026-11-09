// Package dbtest holds fixtures for tests that open a database.
package dbtest

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"

	"github.com/ncruces/go-sqlite3"
)

// Key is a fixed database file key for tests (32 bytes).
var Key = []byte("0123456789abcdef0123456789abcdef")

// Statements counts the statements d runs from now on, e.g. to show a
// listing costs the same few queries for 1 or 40 rows. d keeps a single
// connection, so the count sees every statement; nested transactions
// would wait on it.
func Statements(t testing.TB, d *sql.DB) func() int {
	t.Helper()
	d.SetMaxOpenConns(1)
	c, err := d.Conn(context.Background())
	if err != nil {
		t.Fatalf("statements: %v", err)
	}
	defer c.Close()

	var n atomic.Int64
	count := func(sqlite3.TraceEvent, any, any) error {
		n.Add(1)
		return nil
	}
	err = c.Raw(func(dc any) error {
		return dc.(interface{ Raw() *sqlite3.Conn }).Raw().Trace(sqlite3.TRACE_STMT, count)
	})
	if err != nil {
		t.Fatalf("statements: %v", err)
	}
	return func() int { return int(n.Load()) }
}
