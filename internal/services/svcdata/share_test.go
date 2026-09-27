package svcdata_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"andon/internal/db"
	"andon/internal/db/dbtest"
	"andon/internal/enums"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
)

type gateSource struct {
	calls *atomic.Int32
	gate  chan struct{}
}

func (gateSource) Key() string                { return "test.gate" }
func (gateSource) TTL() time.Duration         { return time.Minute }
func (gateSource) Service() enums.ServiceType { return "" }

func (s gateSource) Fetch(context.Context, sources.Ctx) (any, error) {
	s.calls.Add(1)
	<-s.gate
	return "ok", nil
}

// TestConcurrentGetsShareOneFetch: the analysis run, a tile and a second
// user asking for the same dataset at once cause one call to the service.
func TestConcurrentGetsShareOneFetch(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"), dbtest.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	var calls atomic.Int32
	src := gateSource{&calls, make(chan struct{})}
	sources.Register(src)

	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			res, err := svcdata.Get(context.Background(), d, "test.gate", nil, nil, nil, svcdata.Cached)
			if err != nil || res.Data != "ok" {
				t.Errorf("get: %+v %v", res, err)
			}
		})
	}
	time.Sleep(100 * time.Millisecond)
	close(src.gate)
	wg.Wait()

	if n := calls.Load(); n != 1 {
		t.Fatalf("expected 1 fetch, got %d", n)
	}
}

type paramSource struct{}

func (paramSource) Key() string                { return "test.params" }
func (paramSource) TTL() time.Duration         { return time.Minute }
func (paramSource) Service() enums.ServiceType { return "" }

func (paramSource) Fetch(_ context.Context, sctx sources.Ctx) (any, error) {
	return sctx.Params["q"], nil
}

// TestCacheStaysBounded: every parameter combination (e.g. previews of
// unsaved settings) is a key; the cache must not grow without limit.
func TestCacheStaysBounded(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"), dbtest.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	sources.Register(paramSource{})

	for i := range svcdata.MaxEntries + 50 {
		if _, err := svcdata.Get(context.Background(), d, "test.params", map[string]any{"q": fmt.Sprint(i)}, nil, nil, svcdata.Force); err != nil {
			t.Fatal(err)
		}
	}
	if n := svcdata.Entries(); n > svcdata.MaxEntries {
		t.Fatalf("cache holds %d entries, limit %d", n, svcdata.MaxEntries)
	}
}
