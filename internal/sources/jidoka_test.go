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

// jidokaServer answers like Jidoka's API with n jobs (every tenth one
// failed) and counts the calls to the job list.
func jidokaServer(t *testing.T, n int, jobCalls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.URL.Query().Get("token") != "" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/api/v1/status":
			w.Write([]byte(`{"version":"1.0","commit":"abc","reloaded_at":null,"invalid":1,` +
				`"channels":[{"channel":"mail","down":true,"since":"2026-11-01T08:00:00Z","last_ok":null}],"last_tick":null}`))
		case "/api/v1/jobs":
			jobCalls.Add(1)
			var jobs []string
			for i := 0; i < n; i++ {
				state, reason := "ok", "null"
				if i%10 == 9 {
					state, reason = "fail", `"fail"`
				}
				jobs = append(jobs, fmt.Sprintf(`{"id":"j%d","name":"job %d","kind":"job","state":%q,"reason":%s,"summary":"s%d",`+
					`"last":{"id":%d,"state":%q,"start":"2026-11-02T01:00:00Z","end":"2026-11-02T01:00:05Z"},"next":"2026-11-03T01:00:00Z",`+
					`"tail":"line","url":"https://jidoka.example/jobs/j%d","tags":["nas","night"],"schedule":"0 2 * * *",`+
					`"history":[{"id":%d,"state":"ok","start":"2026-11-01T01:00:00Z","end":null},{"id":%d,"state":%q,"start":"2026-11-02T01:00:00Z","end":null}]}`,
					i, i, state, reason, i, i+100, state, i, i+50, i+100, state))
			}
			jobs = append(jobs, `{"id":"bad","name":"broken","kind":"job","state":"invalid","reason":null,"summary":"","last":null,"next":null,"tail":"","url":""}`)
			w.Write([]byte(`{"jobs":[` + strings.Join(jobs, ",") + `],"total":` + fmt.Sprint(n+1) + `}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestJidokaOneJobRequest: 0, 1 or 50 jobs cost one request to the job
// list; failed jobs are down with the reason failed, an invalid one is
// no heartbeat but counts for the hint.
func TestJidokaOneJobRequest(t *testing.T) {
	for _, n := range []int{0, 1, 50} {
		var calls atomic.Int32
		srv := jidokaServer(t, n, &calls)

		raw, err := sources.JidokaData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "tok", ConnID: 7})
		if err != nil {
			t.Fatalf("%d jobs: %v", n, err)
		}
		if calls.Load() != 1 {
			t.Fatalf("%d jobs: %d requests to /jobs", n, calls.Load())
		}

		data := raw.(*sources.JidokaDataset)
		in := data.Instances[0]
		if got := len(data.Heartbeats()); got != n || in.Invalid != 1 || len(in.InvalidJobs) != 1 || in.InvalidJobs[0] != "broken" {
			t.Fatalf("%d jobs: %d beats, instance %+v", n, got, in)
		}
		if len(in.Channels) != 1 || !in.Channels[0].Down || in.Channels[0].Since.IsZero() || !in.Channels[0].LastOK.IsZero() {
			t.Fatalf("channels %+v", in.Channels)
		}
		if got := len(data.Down()); got != n/10 {
			t.Fatalf("%d jobs: %d down", n, got)
		}
	}
}

// TestJidokaHeartbeat: state, reason, links and times of one job.
func TestJidokaHeartbeat(t *testing.T) {
	var calls atomic.Int32
	srv := jidokaServer(t, 10, &calls)
	raw, err := sources.JidokaData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "tok", ConnID: 7})
	if err != nil {
		t.Fatal(err)
	}

	beats := raw.(*sources.JidokaDataset).Heartbeats()
	ok, bad := beats[0], beats[9]
	if ok.Status != sources.HeartbeatUp || ok.Reason != sources.HeartbeatNoReason || ok.Tool != "jidoka" || ok.ConnID != 7 || ok.NextPing.IsZero() {
		t.Fatalf("ok %+v", ok)
	}
	if bad.Status != sources.HeartbeatDown || bad.Reason != sources.HeartbeatFailed || bad.Summary != "s9" || bad.Tail != "line" ||
		bad.Link != "https://jidoka.example/jobs/j9" || bad.RunLink != "https://jidoka.example/runs/109" ||
		!bad.LastPing.Equal(time.Date(2026, 11, 2, 1, 0, 5, 0, time.UTC)) {
		t.Fatalf("bad %+v", bad)
	}
	if len(bad.Tags) != 2 || bad.Tags[1] != "night" || bad.Schedule != "0 2 * * *" {
		t.Fatalf("tags %v schedule %q", bad.Tags, bad.Schedule)
	}
	if len(bad.History) != 2 || bad.History[0].Status != sources.HeartbeatUp || bad.History[1].Status != sources.HeartbeatDown ||
		bad.History[1].Link != "https://jidoka.example/runs/109" {
		t.Fatalf("history %+v", bad.History)
	}
}

// TestJidokaRefused: a wrong token is an error, shown as the connection's.
func TestJidokaRefused(t *testing.T) {
	var calls atomic.Int32
	srv := jidokaServer(t, 1, &calls)
	if _, err := sources.JidokaData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "wrong"}); err == nil {
		t.Fatal("no error")
	}
}

// TestJidokaMerge: two connections keep their instances apart.
func TestJidokaMerge(t *testing.T) {
	a := &sources.JidokaDataset{Instances: []sources.JidokaInstance{{ConnID: 1, Jobs: []sources.Heartbeat{{Name: "a"}}}}}
	b := &sources.JidokaDataset{Instances: []sources.JidokaInstance{{ConnID: 2, Jobs: []sources.Heartbeat{{Name: "b"}}}}}
	m := a.Merge(b).(*sources.JidokaDataset)
	if len(m.Instances) != 2 || len(m.Heartbeats()) != 2 || len(a.Instances) != 1 {
		t.Fatalf("merge %+v", m)
	}
}

// TestDemoJidoka: the demo reads Studio Weber's jobs from the world: a
// failed job, a missed watch, a paused job, an invalid file, a calm channel.
func TestDemoJidoka(t *testing.T) {
	in := sources.DemoJidoka(time.Now()).Instances[0]
	byName := map[string]sources.Heartbeat{}
	for _, b := range in.Jobs {
		byName[b.Name] = b
	}

	var failed, missed, paused int
	for _, b := range in.Jobs {
		switch {
		case b.Status == sources.HeartbeatDown && b.Reason == sources.HeartbeatFailed:
			failed++
		case b.Status == sources.HeartbeatDown && b.Reason == sources.HeartbeatMissed:
			missed++
		case b.Status == sources.HeartbeatPaused:
			paused++
		}
		if strings.Contains(b.Link, "localhost") || b.Tool != "jidoka" {
			t.Fatalf("beat %+v", b)
		}
	}
	if failed != 2 || missed != 1 || paused != 1 || in.Invalid != 1 || len(in.InvalidJobs) != 1 {
		t.Fatalf("failed %d missed %d paused %d, instance %+v", failed, missed, paused, in)
	}
	for _, c := range in.Channels {
		if c.Down {
			t.Fatalf("channel %+v", c)
		}
	}
}
