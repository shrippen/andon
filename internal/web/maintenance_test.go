package web_test

import (
	"strings"
	"testing"

	"andon/internal/progress"
)

// The maintenance page shows open long tasks, problems and log records.
func TestMaintenancePage(t *testing.T) {
	srv, client, code := newTestServer(t)
	setupAdmin(t, srv, client, code)
	login(t, srv, client)
	progress.Set("test", "admin.logs", nil, 3, 12)
	t.Cleanup(func() { progress.Finish("test") })

	page := string(mustGet(t, srv, client, "/admin/operations"))
	for _, want := range []string{`id="tasks"`, "3 / 12", `--p:25%`, `id="problems"`, `id="recent"`, `id="logs"`} {
		if !strings.Contains(page, want) {
			t.Errorf("%q missing", want)
		}
	}
}
