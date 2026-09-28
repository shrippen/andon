package sources

import "testing"

// Container states from the Engine API's list: health sits in Status,
// the exit code of a stopped container too.
func TestParseDocker(t *testing.T) {
	body := []any{
		map[string]any{"Names": []any{"/immich"}, "Image": "ghcr.io/immich-app/immich-server:v1.132", "State": "running", "Status": "Up 3 days (healthy)"},
		map[string]any{"Names": []any{"/paperless"}, "Image": "paperless", "State": "running", "Status": "Up 2 hours (unhealthy)"},
		map[string]any{"Names": []any{"/backup-job"}, "Image": "restic", "State": "exited", "Status": "Exited (0) 5 hours ago"},
		map[string]any{"Names": []any{"/gitea"}, "Image": "gitea", "State": "exited", "Status": "Exited (137) 2 minutes ago"},
		map[string]any{"Names": []any{"/loop"}, "Image": "x", "State": "restarting", "Status": "Restarting (1) 5 seconds ago"},
	}
	data := parseDocker("http://proxy:2375", body)
	got := map[string]Container{}
	for _, c := range data.Containers {
		got[c.Name] = c
	}
	if got["immich"].Health != HealthHealthy || got["paperless"].Health != HealthUnhealthy {
		t.Fatalf("health: %+v", data.Containers)
	}
	if got["backup-job"].ExitCode != 0 || got["gitea"].ExitCode != 137 || got["loop"].State != "restarting" {
		t.Fatalf("exit: %+v", data.Containers)
	}
}
