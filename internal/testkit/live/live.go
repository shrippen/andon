// Package live runs integration tests against real service instances.
//
// The instances are the connections of the local Andon instance in
// .local-test/ (outside Git): its database data/andon.db, unlocked with
// the master key in secrets/master_key. Writes go to each service's first
// connection.
//
// Services without an own instance run as throwaway containers, listed
// in extra/instances.json. They replace the local instance only when
// asked for (ANDON_LIVE_EXTRA=1, set by scripts/live-extra.sh):
//
//	[{"name": "grocy", "service": "grocy", "url": "http://127.0.0.1:18116", "secret": "…", "options": {…}}]
//
// The tests write to real instances, so they run only when asked for
// (ANDON_LIVE=1, set by `make live`), never with `go test ./...`.
// Without that database or a connection of the service the test is
// skipped too: CI and cloud runs test against the fakes (httptest) only.
//
// Writes touch only entries a test created. Every write is appended to
// .local-test/writes.log (time, service, action, kind, id, test); a
// change to an id not logged as created there fails the test before the
// write is sent.
package live

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
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
	"andon/internal/sources"
)

// Service names an instance by its connection's service type.
type Service string

const (
	Kimai     Service = "kimai"
	Dawarich  Service = "dawarich"
	Ninja     Service = "invoiceninja"
	Paperless Service = "paperless"
	Tandoor   Service = "tandoor"
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
	dirEnv = "ANDON_LOCAL_TEST"
	// runEnv must be set for live tests to run; ninjaEnv as well for
	// writes to Invoice Ninja; extraEnv switches to the throwaway
	// containers.
	runEnv   = "ANDON_LIVE"
	ninjaEnv = "ANDON_LIVE_NINJA"
	extraEnv = "ANDON_LIVE_EXTRA"
	dirName  = ".local-test"
	logName  = "writes.log"

	// The local instance: its database and master key.
	dbPath  = "data/andon.db"
	keyPath = "secrets/master_key"
	// extraPath lists the throwaway instances.
	extraPath = "extra/instances.json"

	// namePrefix marks every entry a test creates, e.g.
	// "andon-test TestPlaces 20261006-143005".
	namePrefix = "andon-test"
)

// ID is an entry's id: a number (Kimai, Paperless) or a hashed key
// (Invoice Ninja).
type ID interface{ ~int | ~int64 | ~string }

// Instance is one connection of the local instance, ready to fetch:
// URL, the login (a personal connection's first holder's), TLS check and
// options. Err is set when its login cannot be read.
type Instance struct {
	Service Service
	Name    string
	Ctx     sources.Ctx
	Err     error
}

// Target is where the instance's writes go.
func (i Instance) Target() outbound.Target {
	return outbound.Target{URL: i.Ctx.URL, Token: i.Ctx.Secret, VerifyTLS: i.Ctx.VerifyTLS}
}

// cache holds each directory's instances: unlocking the database costs
// a second (Argon2id).
var (
	cacheMu sync.Mutex
	cache   = map[string][]Instance{}
)

// Target returns the real instance of svc for writes, or skips the test
// when none is configured. Invoice Ninja hands out invoice and expense
// numbers on save, so its writes run only when asked for explicitly.
func Target(t testing.TB, svc Service) outbound.Target {
	t.Helper()
	if svc == Ninja && os.Getenv(ninjaEnv) == "" {
		t.Skipf("Invoice Ninja writes leave gaps in its number ranges: run only when asked for (%s=1)", ninjaEnv)
	}
	for _, i := range Instances(t) {
		if i.Service != svc {
			continue
		}
		if i.Err != nil {
			t.Fatalf("%s login: %v", svc, i.Err)
		}
		return i.Target()
	}
	t.Skipf("no live %s instance: no %s connection in %s/%s", svc, svc, dirName, dbPath)
	return outbound.Target{}
}

// Instances returns every connection of the local instance, or the
// throwaway containers with ANDON_LIVE_EXTRA, read at most once per
// directory, or skips the test when live tests are not asked for or
// there is no instance.
func Instances(t testing.TB) []Instance {
	t.Helper()
	if os.Getenv(runEnv) == "" {
		t.Skipf("live tests use real instances: run with %s=1 (make live)", runEnv)
	}
	d := dir(t)
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if all, ok := cache[d]; ok {
		return all
	}

	// One source per run: the local instance or the containers.
	load, path := ofLocal, dbPath
	if os.Getenv(extraEnv) != "" {
		load, path = ofExtra, extraPath
	}
	all, err := load(d)
	if err != nil {
		t.Fatalf("read %s/%s: %v", dirName, path, err)
	}
	if len(all) == 0 {
		t.Skipf("no live instance (%s/%s)", dirName, path)
	}
	cache[d] = all
	return all
}

// ofLocal reads the local instance's connections; none without its
// database or master key.
func ofLocal(d string) ([]Instance, error) {
	path := filepath.Join(d, dbPath)
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	key, err := os.ReadFile(filepath.Join(d, keyPath))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return read(path, strings.TrimSpace(string(key)))
}

// ofExtra reads the throwaway instances; none without the file.
func ofExtra(d string) ([]Instance, error) {
	raw, err := os.ReadFile(filepath.Join(d, extraPath))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []struct {
		Name, Service, URL, Secret string
		Options                    map[string]any
		VerifyTLS                  bool `json:"verify_tls"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	out := make([]Instance, 0, len(list))
	for _, e := range list {
		out = append(out, Instance{Service: Service(e.Service), Name: e.Name,
			Ctx: sources.Ctx{URL: strings.TrimRight(e.URL, "/"), Secret: e.Secret, VerifyTLS: e.VerifyTLS, Options: e.Options}})
	}
	return out, nil
}

// read unlocks the database and returns its connections with their
// logins: a shared one's own, a personal one's first holder's.
func read(path, key string) ([]Instance, error) {
	d, _, err := maintenance.Unlock(path, key)
	if err != nil {
		return nil, err
	}
	defer d.Close()

	var conns []*model.Connection
	var holders map[int64][]model.Holder
	err = db.WithRead(d, func(tx *sql.Tx) error {
		if conns, err = content.AllConnections(tx); err != nil {
			return err
		}
		holders, err = content.CredentialHolders(tx)
		return err
	})
	if err != nil {
		return nil, err
	}

	all := make([]Instance, 0, len(conns))
	for _, c := range conns {
		h := model.NoHolder
		if c.CredentialMode == enums.CredentialPersonal && len(holders[c.ID]) > 0 {
			h = holders[c.ID][0]
		}
		secret, err := svcdata.Secret(context.Background(), d, c, h)
		all = append(all, Instance{
			Service: Service(c.Service), Name: c.Name, Err: err,
			Ctx: sources.Ctx{URL: strings.TrimRight(c.URL, "/"), Secret: secret, VerifyTLS: c.VerifyTLS, Options: c.Options},
		})
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
