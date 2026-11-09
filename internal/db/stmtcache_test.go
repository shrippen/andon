package db

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"andon/internal/db/dbtest"
)

// openStmtDB opens a fresh database for the statement cache tests.
func openStmtDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "test.db"), testKey)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// TestStatementsReused: a query run again reuses its prepared statement,
// in and outside transactions, for reads and writes (SQLite parsed every
// query anew: 13 % of a page's time).
func TestStatementsReused(t *testing.T) {
	d := openStmtDB(t)
	const read = "SELECT COUNT(*) FROM users WHERE id > ?"
	const write = "INSERT INTO instance_settings (key, value) VALUES (?, ?)"
	rec := dbtest.Trace(t, d)

	for i := range 5 {
		var n int
		if err := d.QueryRow(read, i).Scan(&n); err != nil {
			t.Fatal(err)
		}
		err := WithRead(d, func(tx *sql.Tx) error { return tx.QueryRow(read, i).Scan(&n) })
		if err != nil {
			t.Fatal(err)
		}
		err = WithTx(d, func(tx *sql.Tx) error {
			_, err := tx.Exec(write, fmt.Sprintf("stmt-test-%d", i), "x")
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{read, write} {
		if got := rec.Prepared(q); got != 1 {
			t.Errorf("%s: prepared %d times, want once", q, got)
		}
	}
}

// TestStatementsNested: the same query open twice at once on one
// connection gets two statements; both read their own rows.
func TestStatementsNested(t *testing.T) {
	d := openStmtDB(t)
	const q = "SELECT value FROM (SELECT 1 AS value UNION ALL SELECT 2 UNION ALL SELECT 3) WHERE value >= ? ORDER BY value"
	err := WithRead(d, func(tx *sql.Tx) error {
		outer, err := tx.Query(q, 1)
		if err != nil {
			return err
		}
		defer outer.Close()

		var seen []string
		for outer.Next() {
			var a int
			if err := outer.Scan(&a); err != nil {
				return err
			}
			var b int
			if err := tx.QueryRow(q, a+1).Scan(&b); err != nil && err != sql.ErrNoRows {
				return err
			}
			seen = append(seen, fmt.Sprintf("%d:%d", a, b))
		}
		if fmt.Sprint(seen) != "[1:2 2:3 3:0]" {
			t.Errorf("nested reads: %v", seen)
		}
		return outer.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestStatementsLeftOpen: rows closed before their end leave a statement
// that starts over on its next use.
func TestStatementsLeftOpen(t *testing.T) {
	d := openStmtDB(t)
	const q = "SELECT value FROM (SELECT 1 AS value UNION ALL SELECT 2) ORDER BY value"
	for range 3 {
		rows, err := d.Query(q)
		if err != nil {
			t.Fatal(err)
		}
		var v int
		if !rows.Next() || rows.Scan(&v) != nil || v != 1 {
			t.Fatalf("first row: %d (%v)", v, rows.Err())
		}
		rows.Close()
	}
}

// TestStatementsBounded: more distinct queries than a connection keeps
// still run, and the database closes with statements kept.
func TestStatementsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := Open(path, testKey)
	if err != nil {
		t.Fatal(err)
	}
	for i := range stmtsKept * 2 {
		var n int
		if err := d.QueryRow(fmt.Sprintf("SELECT %d + ?", i), 1).Scan(&n); err != nil || n != i+1 {
			t.Fatalf("query %d: %d %v", i, n, err)
		}
	}
	if err := d.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestReadKeepsStatements: a read transaction keeps the prepared
// statements. Switching query_only on and off around it expired all of
// them, so each query was compiled again on its first step (59 % of a
// read).
func TestReadKeepsStatements(t *testing.T) {
	d := openStmtDB(t)
	const q = "SELECT COUNT(*) FROM users WHERE id > ?"
	read := func() {
		err := WithRead(d, func(tx *sql.Tx) error {
			var n int
			return tx.QueryRow(q, 0).Scan(&n)
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	rec := dbtest.Trace(t, d)
	read() // compiles: tracing expires the statements
	first := rec.Compiles()
	for range 3 {
		read()
	}
	if got := rec.Compiles() - first; got != 0 {
		t.Errorf("3 more reads of a kept query compiled it %d times", got)
	}
}

// TestReadRefusesWrites: a write in a read transaction fails, with and
// without arguments, and changes nothing.
func TestReadRefusesWrites(t *testing.T) {
	d := openStmtDB(t)
	for _, w := range []struct {
		query string
		args  []any
	}{
		{"INSERT INTO instance_settings (key, value) VALUES (?, ?)", []any{"read-write", "x"}},
		{"INSERT INTO instance_settings (key, value) VALUES ('read-write', 'x')", nil},
	} {
		err := WithRead(d, func(tx *sql.Tx) error {
			_, err := tx.Exec(w.query, w.args...)
			return err
		})
		if err == nil {
			t.Errorf("%s: wrote in a read transaction", w.query)
		}
	}
	var n int
	if err := d.QueryRow("SELECT COUNT(*) FROM instance_settings WHERE key = 'read-write'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("rows written: %d %v", n, err)
	}
	// The connection takes writes again after the read.
	if _, err := d.Exec("INSERT INTO instance_settings (key, value) VALUES ('after-read', 'x')"); err != nil {
		t.Fatalf("write after read: %v", err)
	}
}
