package httpclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"andon/internal/drivers/httpclient"
)

// TestMeterReadsRateLimits: calls are counted, the tightest quota kept,
// and a 429 with Retry-After marks the fetch refused until then.
func TestMeterReadsRateLimits(t *testing.T) {
	reset := time.Now().Add(time.Hour).Unix()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/core":
			w.Header().Set("X-RateLimit-Limit", "5000")
			w.Header().Set("X-RateLimit-Remaining", "4000")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
		case "/search":
			w.Header().Set("X-RateLimit-Limit", "30")
			w.Header().Set("X-RateLimit-Remaining", "3")
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
		case "/busy":
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	ctx, m := httpclient.Metered(context.Background())
	for _, path := range []string{"/core", "/search", "/core"} {
		if _, _, err := httpclient.GetJSON(ctx, srv.URL+path, httpclient.Options{}); err != nil {
			t.Fatal(err)
		}
	}
	u := m.Usage()
	if u.Calls != 3 || u.Limit != 30 || u.Remaining != 3 || u.Reset.Unix() != reset || u.Limited {
		t.Fatalf("usage %+v", u)
	}

	httpclient.GetJSON(ctx, srv.URL+"/busy", httpclient.Options{})
	u = m.Usage()
	if wait := time.Until(u.RetryAt); !u.Limited || wait < 110*time.Second || wait > 125*time.Second {
		t.Fatalf("refused %+v", u)
	}
}
