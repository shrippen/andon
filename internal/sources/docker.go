package sources

// Docker: every container of a host, read through a read-only socket
// proxy (containers only). Registers "docker.data" and "docker.test".
//
//	Status "Up 3 days (unhealthy)"  → Health unhealthy
//	Status "Exited (137) 2 min ago" → ExitCode 137

import (
	"context"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"andon/internal/drivers/services"
	"andon/internal/enums"
)

// Container health as Docker reports it in the status text.
const (
	HealthNone      = ""
	HealthHealthy   = "healthy"
	HealthUnhealthy = "unhealthy"
	HealthStarting  = "starting"
)

// Container states of the Engine API.
const (
	StateRunning    = "running"
	StateExited     = "exited"
	StateRestarting = "restarting"
)

type Container struct {
	Name, Image, State, Status string
	Health                     string
	ExitCode                   int // of an exited container
}

type DockerDataset struct {
	URL        string
	Containers []Container
}

var DockerData = source{key: "docker.data", ttl: opsTTL, service: enums.ServiceDocker, fetch: fetchDocker}

func fetchDocker(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoDocker(), nil
	}
	body, err := services.DockerApi{URL: sctx.URL, Verify: sctx.VerifyTLS}.Containers(ctx)
	if err != nil {
		return nil, fetchError(err)
	}
	return parseDocker(sctx.URL, body), nil
}

var exitCode = regexp.MustCompile(`^Exited \((\d+)\)`)

func parseDocker(base string, body any) *DockerDataset {
	data := &DockerDataset{URL: base}
	for _, raw := range asList(body) {
		m := asMap(raw)
		names := asList(m["Names"])
		if len(names) == 0 {
			continue
		}
		c := Container{Name: strings.TrimPrefix(asStr(names[0]), "/"), Image: asStr(m["Image"]),
			State: asStr(m["State"]), Status: asStr(m["Status"])}
		switch {
		case strings.Contains(c.Status, "(unhealthy)"):
			c.Health = HealthUnhealthy
		case strings.Contains(c.Status, "(healthy)"):
			c.Health = HealthHealthy
		case strings.Contains(c.Status, "(health: starting)"):
			c.Health = HealthStarting
		}
		if sub := exitCode.FindStringSubmatch(c.Status); sub != nil {
			c.ExitCode, _ = strconv.Atoi(sub[1])
		}
		data.Containers = append(data.Containers, c)
	}
	sort.Slice(data.Containers, func(i, j int) bool { return data.Containers[i].Name < data.Containers[j].Name })
	return data
}

// DemoDocker is the demo Docker dataset: one unhealthy, one crashed.
func DemoDocker() *DockerDataset {
	return &DockerDataset{URL: "http://docker-proxy.demo:2375", Containers: []Container{
		{Name: "gitea", Image: "gitea/gitea:1.24", State: StateRunning, Status: "Up 6 days (healthy)", Health: HealthHealthy},
		{Name: "immich-server", Image: "ghcr.io/immich-app/immich-server:v1.131.0", State: StateRunning, Status: "Up 2 days (healthy)", Health: HealthHealthy},
		{Name: "paperless", Image: "paperlessngx/paperless-ngx", State: StateRunning, Status: "Up 3 hours (unhealthy)", Health: HealthUnhealthy},
		{Name: "restic-nightly", Image: "restic/restic", State: StateExited, Status: "Exited (0) 5 hours ago"},
		{Name: "umami", Image: "ghcr.io/umami-software/umami", State: StateExited, Status: "Exited (137) 40 minutes ago", ExitCode: 137},
	}}
}

func init() {
	Register(DockerData)
	Register(testOf{DockerData, func(d any) map[string]any { return map[string]any{"containers": len(d.(*DockerDataset).Containers)} }})
}
