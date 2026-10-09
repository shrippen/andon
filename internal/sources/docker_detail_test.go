package sources_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"andon/internal/sources"
)

// TestDockerDetailStatsAtOnce: Docker answers stats?stream=false only
// after a second (it measures the CPU), so 30 running containers six at a
// time kept the dialog loading for over five seconds. The stats calls
// wait on Docker, not on Andon: they run together.
func TestDockerDetailStatsAtOnce(t *testing.T) {
	const running = 30
	var now, peak atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/containers/json"):
			var list []string
			for i := range running {
				list = append(list, fmt.Sprintf(`{"Id":"c%d","Names":["/c%d"],"Image":"img","State":"running","Status":"Up 1 hour"}`, i, i))
			}
			w.Write([]byte("[" + strings.Join(list, ",") + "]"))
		case strings.HasSuffix(r.URL.Path, "/stats"):
			n := now.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			time.Sleep(100 * time.Millisecond)
			now.Add(-1)
			w.Write([]byte(`{"cpu_stats":{},"precpu_stats":{},"memory_stats":{}}`))
		default:
			w.Write([]byte(`{"RestartCount":0,"State":{}}`))
		}
	}))
	defer srv.Close()

	raw, err := sources.DockerDetailSource.Fetch(context.Background(), sources.Ctx{URL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(raw.(*sources.DockerDetail).Info); got != running {
		t.Fatalf("info for %d containers, want %d", got, running)
	}
	if peak.Load() < running/2 {
		t.Fatalf("at most %d stats calls at once, want them together", peak.Load())
	}
}
