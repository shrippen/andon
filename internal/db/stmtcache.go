package db

import (
	"container/list"
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"

	sqlite "github.com/ncruces/go-sqlite3/driver"
)

// The driver (SQLite in WebAssembly) parsed every query anew: 13 % of a
// page's time. Each connection now keeps its prepared statements:
//
//	database/sql ─Prepare(q)─► cachingConn ─idle stmt of q?─► reuse
//	                                        └─none─► driver Prepare
//	stmt.Close ─► reset, kept idle (the stmtsKept last used), else closed
//
// Read transactions begin here, not in the driver: it switched PRAGMA
// query_only on and off around each, and every switch expires all
// prepared statements of the connection. Instead a read transaction
// takes only statements SQLite calls read-only.
//
// database/sql uses a connection from one goroutine at a time, so the
// cache needs no lock.

// stmtsKept is how many idle statements a connection keeps.
const stmtsKept = 128

// errReadOnly is a write in a read transaction (WithRead).
var errReadOnly = errors.New("db: write in a read-only transaction")

// errNoStmt means the driver's statement lacks what the cache needs: a
// newer driver changed it.
var errNoStmt = errors.New("db: driver statement cannot be kept")

// openDB opens dsn with the statement cache.
func openDB(dsn string) (*sql.DB, error) {
	c, err := (&sqlite.SQLite{}).OpenConnector(dsn)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(cachingConnector{c}), nil
}

type cachingConnector struct{ inner driver.Connector }

func (c cachingConnector) Driver() driver.Driver { return c.inner.Driver() }

func (c cachingConnector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := c.inner.Connect(ctx)
	if err != nil {
		return nil, err
	}
	inner, ok := conn.(innerConn)
	if !ok {
		conn.Close()
		return nil, driver.ErrBadConn
	}
	return &cachingConn{innerConn: inner, idle: map[string]*list.Element{}, lru: list.New()}, nil
}

// innerConn is what the driver's connection offers; cachingConn passes
// all of it on (Raw too, for backups and tests).
type innerConn interface {
	sqlite.Conn
	driver.ExecerContext
	driver.NamedValueChecker
}

// innerStmt is the driver's statement; Reset and ClearBindings come
// from its sqlite3.Stmt.
type innerStmt interface {
	driver.Stmt
	driver.StmtExecContext
	driver.StmtQueryContext
	driver.NamedValueChecker
	Reset() error
	ClearBindings() error
	ReadOnly() bool
}

type cachingConn struct {
	innerConn
	idle    map[string]*list.Element // query → its idle statement in lru
	lru     *list.List               // idle statements, last used in front
	reading bool                     // in a read transaction
}

// BeginTx begins a read transaction as a plain deferred BEGIN; others
// go to the driver.
func (c *cachingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if !opts.ReadOnly || opts.Isolation != driver.IsolationLevel(sql.LevelDefault) {
		return c.innerConn.BeginTx(ctx, opts)
	}
	if err := c.Raw().Exec("BEGIN DEFERRED"); err != nil {
		return nil, err
	}
	c.reading = true
	return readTx{c}, nil
}

// readTx ends a read transaction; a rollback runs even when the
// request's context was cancelled.
type readTx struct{ c *cachingConn }

func (t readTx) Commit() error   { return t.c.end("COMMIT") }
func (t readTx) Rollback() error { return t.c.end("ROLLBACK") }

func (c *cachingConn) end(query string) error {
	c.reading = false
	raw := c.Raw()
	if old := raw.SetInterrupt(context.Background()); old != context.Background() {
		defer raw.SetInterrupt(old)
	}
	return raw.Exec(query)
}

// ExecContext leaves a read transaction's statements to PrepareContext,
// which refuses writes.
func (c *cachingConn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.reading {
		return nil, driver.ErrSkip
	}
	return c.innerConn.ExecContext(ctx, query, args)
}

// Prepare: see PrepareContext.
func (c *cachingConn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

// PrepareContext hands out the idle statement of query, else prepares
// one.
func (c *cachingConn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	s, err := c.prepare(ctx, query)
	if err != nil {
		return nil, err
	}
	if c.reading && !s.ReadOnly() {
		s.Close()
		return nil, errReadOnly
	}
	return s, nil
}

// prepare takes query's idle statement, else prepares one.
func (c *cachingConn) prepare(ctx context.Context, query string) (*cachedStmt, error) {
	if e, ok := c.idle[query]; ok {
		delete(c.idle, query)
		c.lru.Remove(e)
		return e.Value.(*cachedStmt), nil
	}

	s, err := c.innerConn.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	inner, ok := s.(innerStmt)
	if !ok {
		s.Close()
		return nil, errNoStmt
	}
	return &cachedStmt{innerStmt: inner, conn: c, query: query}, nil
}

// Close closes the idle statements first: SQLite refuses to close a
// connection that still has some.
func (c *cachingConn) Close() error {
	for e := c.lru.Front(); e != nil; e = e.Next() {
		e.Value.(*cachedStmt).innerStmt.Close()
	}
	c.idle, c.lru = nil, list.New()
	return c.innerConn.Close()
}

// keep takes a closed statement back as idle; a second one of the same
// query, or one that does not reset, is closed.
func (c *cachingConn) keep(s *cachedStmt) error {
	if c.idle == nil {
		return s.innerStmt.Close()
	}
	if _, ok := c.idle[s.query]; ok {
		return s.innerStmt.Close()
	}
	if s.innerStmt.Reset() != nil || s.innerStmt.ClearBindings() != nil {
		return s.innerStmt.Close()
	}

	c.idle[s.query] = c.lru.PushFront(s)
	if c.lru.Len() <= stmtsKept {
		return nil
	}
	oldest := c.lru.Remove(c.lru.Back()).(*cachedStmt)
	delete(c.idle, oldest.query)
	return oldest.innerStmt.Close()
}

// cachedStmt goes back to its connection on Close.
type cachedStmt struct {
	innerStmt
	conn  *cachingConn
	query string
}

func (s *cachedStmt) Close() error { return s.conn.keep(s) }
