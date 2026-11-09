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

// settle is how long Trace waits for connections in use.
const settle = 5 * time.Second

// Record is what d ran since Trace: statements and the rows they
// returned, e.g. to show a listing costs the same few queries for 1 or
// 40 connections.
type Record struct {
	mu    sync.Mutex
	stmts []string
	rows  int
}

// Statements are the SQL texts run, in order.
func (r *Record) Statements() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.stmts)
}

// Rows counts the result rows read.
func (r *Record) Rows() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.rows
}

func (r *Record) add(evt sqlite3.TraceEvent, _, sql any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if evt == sqlite3.TRACE_ROW {
		r.rows++
		return nil
	}
	r.stmts = append(r.stmts, fmt.Sprint(sql))
	return nil
}

// Trace records what d runs from now on. d keeps a single connection, so
// the record sees everything; nested transactions would wait on it.
func Trace(t testing.TB, d *sql.DB) *Record {
	t.Helper()
	// One connection, the traced one: wait for background work to hand
	// its connection back, else the pool may keep that one and close ours.
	for deadline := time.Now().Add(settle); d.Stats().InUse > 0; time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("trace: %d connections stay in use", d.Stats().InUse)
		}
	}
	d.SetMaxOpenConns(1)
	d.SetMaxIdleConns(1)
	c, err := d.Conn(context.Background())
	if err != nil {
		t.Fatalf("trace: %v", err)
	}
	defer c.Close()

	r := &Record{}
	err = c.Raw(func(dc any) error {
		return dc.(interface{ Raw() *sqlite3.Conn }).Raw().Trace(sqlite3.TRACE_STMT|sqlite3.TRACE_ROW, r.add)
	})
	if err != nil {
		t.Fatalf("trace: %v", err)
	}
	return r
}
