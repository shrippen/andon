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
	mu       sync.Mutex
	stmts    []string
	rows     int
	prepared map[string]map[*sqlite3.Stmt]bool // SQL → statements that ran it
	compiles int
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

// Prepared counts the distinct prepared statements that ran query: 1
// when every run reused the first.
func (r *Record) Prepared(query string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.prepared[query])
}

// Compiles counts the SELECTs SQLite compiled, again after an expiry
// too: 1 for a query run thrice from a kept statement.
func (r *Record) Compiles() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.compiles
}

// authorize counts compiled SELECTs; SQLite asks it while compiling.
func (r *Record) authorize(action sqlite3.AuthorizerActionCode, _, _, _, _ string) sqlite3.AuthorizerReturnCode {
	if action == sqlite3.AUTH_SELECT {
		r.mu.Lock()
		r.compiles++
		r.mu.Unlock()
	}
	return sqlite3.AUTH_OK
}

func (r *Record) add(evt sqlite3.TraceEvent, stmt, sql any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if evt == sqlite3.TRACE_ROW {
		r.rows++
		return nil
	}
	text := fmt.Sprint(sql)
	r.stmts = append(r.stmts, text)
	if s, ok := stmt.(*sqlite3.Stmt); ok {
		if r.prepared[text] == nil {
			r.prepared[text] = map[*sqlite3.Stmt]bool{}
		}
		r.prepared[text][s] = true
	}
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

	r := &Record{prepared: map[string]map[*sqlite3.Stmt]bool{}}
	err = c.Raw(func(dc any) error {
		raw := dc.(interface{ Raw() *sqlite3.Conn }).Raw()
		if err := raw.SetAuthorizer(r.authorize); err != nil {
			return err
		}
		return raw.Trace(sqlite3.TRACE_STMT|sqlite3.TRACE_ROW, r.add)
	})
	if err != nil {
		t.Fatalf("trace: %v", err)
	}
	return r
}
