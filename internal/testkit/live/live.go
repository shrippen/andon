// Package live runs integration tests against real service instances.
//
// Connections and data live in .local-test/ (outside Git), one file per
// service, e.g. .local-test/kimai.json:
//
//	{"url": "https://kimai.example", "token": "…", "verifyTLS": true}
//
// Without that file the test is skipped: CI and cloud runs test against
// the fakes (httptest) only.
//
// Writes touch only entries a test created. Every write is appended to
// .local-test/writes.log (time, service, action, kind, id, test); a
// change to an id not logged as created there fails the test before the
// write is sent.
package live

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"andon/internal/outbound"
)

// Service names a configured instance; it is also the config file name.
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

	// namePrefix marks every entry a test creates, e.g.
	// "andon-test TestPlaces 20261006-143005".
	namePrefix = "andon-test"
)

// ID is an entry's id: a number (Kimai, Paperless) or a hashed key
// (Invoice Ninja).
type ID interface{ ~int | ~int64 | ~string }

// conf is one service's file in .local-test/.
type conf struct {
	URL       string `json:"url"`
	Token     string `json:"token"`
	VerifyTLS *bool  `json:"verifyTLS"`
}

// Target returns the real instance of svc, or skips the test when none
// is configured.
func Target(t testing.TB, svc Service) outbound.Target {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir(t), string(svc)+".json"))
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("no live %s instance (%s/%s.json)", svc, dirName, svc)
	}
	if err != nil {
		t.Fatalf("read %s config: %v", svc, err)
	}

	var c conf
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatalf("parse %s config: %v", svc, err)
	}
	if c.URL == "" {
		t.Fatalf("%s config has no url", svc)
	}

	// TLS is checked unless the file says otherwise.
	verify := c.VerifyTLS == nil || *c.VerifyTLS
	return outbound.Target{URL: strings.TrimRight(c.URL, "/"), Token: c.Token, VerifyTLS: verify}
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
