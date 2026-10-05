package svcdata_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"andon/internal/db"
	"andon/internal/db/dbtest"
	"andon/internal/drivers/httpclient"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/services/svcdata"
	"andon/internal/sources"
)

// paceSource asks a test server whatever it answers.
type paceSource struct{ url string }

func (paceSource) Key() string                { return "test.pace" }
func (paceSource) TTL() time.Duration         { return time.Minute }
func (paceSource) Service() enums.ServiceType { return "" }

func (s paceSource) Fetch(ctx context.Context, _ sources.Ctx) (any, error) {
	body, _, err := httpclient.GetJSON(ctx, s.url, httpclient.Options{})
	return body, err
}

// TestRateLimitPausesQuery: a 429 with Retry-After holds the query back
// until then; reads in between get the cache, not the service.
func TestRateLimitPausesQuery(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"), dbtest.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "600")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	sources.Register(paceSource{srv.URL})

	var last svcdata.Result
	for range 3 {
		if last, err = svcdata.Get(context.Background(), d, "test.pace", nil, nil, model.NoHolder, svcdata.Cached); err != nil {
			t.Fatal(err)
		}
	}
	wait := time.Until(last.NextAt)
	if calls != 1 || wait < 9*time.Minute || wait > 11*time.Minute || svcdata.Due("test.pace", last, time.Now()) {
		t.Fatalf("calls=%d next in %v", calls, wait)
	}
}
