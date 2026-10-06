// Package live runs integration tests against real service instances.
//
// The instances are the connections of the local Andon instance in
// .local-test/ (outside Git): its database data/andon.db, unlocked with
// the master key in secrets/master_key. Each service's first connection
// with a shared login is used.
//
// Without that database or a connection of the service the test is
// skipped: CI and cloud runs test against the fakes (httptest) only.
//
// Writes touch only entries a test created. Every write is appended to
// .local-test/writes.log (time, service, action, kind, id, test); a
// change to an id not logged as created there fails the test before the
// write is sent.
package live

import (
	"bufio"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/outbound"
	"andon/internal/repos/content"
	"andon/internal/services/maintenance"
	"andon/internal/services/svcdata"
)

// Service names an instance by its connection's service type.
type Service string

const (
	Kimai     Service = "kimai"
	Dawarich  Service = "dawarich"
	Ninja     Service = "invoiceninja"
	Paperless Service = "paperless"
)

// Action is one kind of write in the log.
type Action string

const (
	Create Action = "create"
	Update Action = "update"
	Delete Action = "delete"
)

const (
	// dirEnv overrides the directory (tests of this package).
	dirEnv  = "ANDON_LOCAL_TEST"
	dirName = ".local-test"
	logName = "writes.log"

	// The local instance: its database and master key.
	dbPath  = "data/andon.db"
	keyPath = "secrets/master_key"

	// namePrefix marks every entry a test creates, e.g.
	// "andon-test TestPlaces 20261006-143005".
	namePrefix = "andon-test"
)

// ID is an entry's id: a number (Kimai, Paperless) or a hashed key
// (Invoice Ninja).
type ID interface{ ~int | ~int64 | ~string }

// targets caches each directory's instances: unlocking the database
// costs a second (Argon2id).
var (
	targetsMu sync.Mutex
	targets   = map[string]map[Service]outbound.Target{}
)

// Target returns the real instance of svc, or skips the test when none
// is configured.
func Target(t testing.TB, svc Service) outbound.Target {
	t.Helper()
	all := instances(t)
	to, ok := all[svc]
	if !ok {
		t.Skipf("no live %s instance: no shared %s connection in %s/%s", svc, svc, dirName, dbPath)
	}
	return to
}

// instances reads the shared connections of the local instance, at most
// once per directory.
func instances(t testing.TB) map[Service]outbound.Target {
	t.Helper()
	d := dir(t)
	targetsMu.Lock()
	defer targetsMu.Unlock()
	if all, ok := targets[d]; ok {
		return all
	}

	path := filepath.Join(d, dbPath)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		t.Skipf("no live instance (%s/%s)", dirName, dbPath)
	}
	key, err := os.ReadFile(filepath.Join(d, keyPath))
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("no master key (%s/%s)", dirName, keyPath)
	}
	if err != nil {
		t.Fatalf("read master key: %v", err)
	}

	all, err := read(path, strings.TrimSpace(string(key)))
	if err != nil {
		t.Fatalf("read live instance: %v", err)
	}
	targets[d] = all
	return all
}

// read unlocks the database and returns each service's first connection
// with a shared login and its secret.
func read(path, key string) (map[Service]outbound.Target, error) {
	d, _, err := maintenance.Unlock(path, key)
	if err != nil {
		return nil, err
	}
	defer d.Close()

	var conns []*model.Connection
	err = db.WithRead(d, func(tx *sql.Tx) error {
		conns, err = content.AllConnections(tx)
		return err
	})
	if err != nil {
		return nil, err
	}

	all := map[Service]outbound.Target{}
	for _, c := range conns {
		svc := Service(c.Service)
		if _, seen := all[svc]; seen || c.CredentialMode != enums.CredentialShared {
			continue
		}
		secret, err := svcdata.Secret(d, c, model.NoHolder)
		if err != nil {
			return nil, fmt.Errorf("%s connection %d: %w", svc, c.ID, err)
		}
		all[svc] = outbound.Target{URL: strings.TrimRight(c.URL, "/"), Token: secret, VerifyTLS: c.VerifyTLS}
	}
	return all, nil
}

// Name returns a fresh name for an entry this test creates.
func Name(t testing.TB) string {
	return fmt.Sprintf("%s %s %s", namePrefix, t.Name(), time.Now().Format("20060102-150405"))
}

// Created logs an entry the test has just created.
func Created[I ID](t testing.TB, svc Service, kind string, id I) {
	t.Helper()
	write(t, svc, Create, kind, id)
}

// Change checks that id is the test's own entry and logs the write;
// call it before sending the write.
func Change[I ID](t testing.TB, svc Service, action Action, kind string, id I) {
	t.Helper()
	if !own(t, svc, kind, id) {
		t.Fatalf("%s %s %v was not created by a test: refusing %s", svc, kind, id, action)
	}
	write(t, svc, action, kind, id)
}

// own reports whether the log holds the creation of svc's kind id.
func own[I ID](t testing.TB, svc Service, kind string, id I) bool {
	t.Helper()
	f, err := os.Open(filepath.Join(dir(t), logName))
	if errors.Is(err, os.ErrNotExist) {
		return false
	}
	if err != nil {
		t.Fatalf("read write log: %v", err)
	}
	defer f.Close()

	// Fields: time, service, action, kind, id, test.
	want := []string{string(svc), string(Create), kind, fmt.Sprint(id)}
	lines := bufio.NewScanner(f)
	for lines.Scan() {
		fields := strings.Split(lines.Text(), "\t")
		if len(fields) < 5 {
			continue
		}
		if strings.Join(fields[1:5], "\t") == strings.Join(want, "\t") {
			return true
		}
	}
	return false
}

// write appends one line to the write log.
func write[I ID](t testing.TB, svc Service, action Action, kind string, id I) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir(t), logName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open write log: %v", err)
	}
	defer f.Close()

	line := strings.Join([]string{time.Now().Format(time.RFC3339), string(svc), string(action), kind, fmt.Sprint(id), t.Name()}, "\t")
	if _, err := fmt.Fprintln(f, line); err != nil {
		t.Fatalf("write log: %v", err)
	}
}

// dir finds .local-test/ in the module root (go test runs in the
// package directory).
func dir(t testing.TB) string {
	t.Helper()
	if d := os.Getenv(dirEnv); d != "" {
		return d
	}

	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("working dir: %v", err)
	}
	for d := wd; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return filepath.Join(d, dirName)
		}
		if filepath.Dir(d) == d {
			t.Fatalf("no go.mod above %s", wd)
		}
	}
}
