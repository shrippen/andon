package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"andon/internal/sources"
)

// TestVikunjaVersions: Vikunja 2 lists tasks at GET api/v1/tasks and
// reads "all" in api/v1/tasks/all as a task id (400); before 2 only
// tasks/all lists them. Both give the open and the done task.
func TestVikunjaVersions(t *testing.T) {
	for _, tc := range []struct{ name, list string }{{"v2", "/api/v1/tasks"}, {"v0.24", "/api/v1/tasks/all"}} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer tok" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				switch r.URL.Path {
				case "/api/v1/projects":
					w.Write([]byte(`[{"id":2,"title":"Studio"}]`))
				case tc.list:
					if r.URL.Query().Get("filter") == "done = false" {
						w.Write([]byte(`[{"title":"Offen","project_id":2,"done":false,"due_date":"2026-10-01T10:00:00Z","done_at":"0001-01-01T00:00:00Z"}]`))
						return
					}
					w.Write([]byte(`[{"title":"Erledigt","project_id":2,"done":true,"due_date":"0001-01-01T00:00:00Z","done_at":"2026-10-08T10:00:00Z"}]`))
				default:
					w.WriteHeader(http.StatusBadRequest)
				}
			}))
			defer srv.Close()

			raw, err := sources.VikunjaData.Fetch(context.Background(), sources.Ctx{URL: srv.URL, Secret: "tok"})
			if err != nil {
				t.Fatal(err)
			}
			d := raw.(*sources.VikunjaDataset)
			if len(d.Tasks) != 2 || d.Tasks[0].Project != "Studio" || d.Tasks[0].Due.IsZero() || !d.Tasks[1].Done {
				t.Fatalf("tasks %+v", d.Tasks)
			}
		})
	}
}
