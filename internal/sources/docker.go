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
	"sync"

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
	ID                         string
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
		c := Container{ID: asStr(m["Id"]), Name: strings.TrimPrefix(asStr(names[0]), "/"), Image: asStr(m["Image"]),
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

// ContainerInfo is what a dialog adds to a container when it opens.
type ContainerInfo struct {
	Restarts   int
	OOMKilled  bool     // the kernel ended it for lack of memory (exit 137)
	CPU        float64  // percent of one core; running only
	MemMB      float64  // running only
	MemLimitMB float64  // 0 = no limit
	Logs       []string // problem containers only: the last lines, secrets masked
}

// DockerDetail is the dialog's extra per container name.
type DockerDetail struct{ Info map[string]ContainerInfo }

var DockerDetailSource = source{key: "docker.detail", ttl: detailTTL, service: enums.ServiceDocker, fetch: fetchDockerDetail}

// Limits of the dialog's fetch: log lines, containers with stats, calls
// at once.
const (
	dockerLogLines = 20
	dockerStatsMax = 30
	dockerParallel = 6
	bytesPerMB     = 1024 * 1024
)

func fetchDockerDetail(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoDockerDetail(), nil
	}
	api := services.DockerApi{URL: sctx.URL, Verify: sctx.VerifyTLS}
	body, err := api.Containers(ctx)
	if err != nil {
		return nil, fetchError(err)
	}
	list := parseDocker(sctx.URL, body).Containers
	out := &DockerDetail{Info: map[string]ContainerInfo{}}
	var mu sync.Mutex
	parallel(ctx, len(list), dockerParallel, func(i int) {
		c := list[i]
		info := ContainerInfo{}
		if raw, err := api.Inspect(ctx, c.ID); err == nil {
			info.Restarts = int(asFloat(asMap(raw)["RestartCount"]))
			info.OOMKilled = asBool(asMap(asMap(raw)["State"])["OOMKilled"])
		}
		problem := c.State == StateRestarting || (c.State == StateExited && c.ExitCode != 0) || c.Health == HealthUnhealthy
		if problem {
			if text, err := api.Logs(ctx, c.ID, dockerLogLines); err == nil {
				info.Logs = logLines(text)
			}
		}
		if c.State == StateRunning && i < dockerStatsMax {
			if raw, err := api.Stats(ctx, c.ID); err == nil {
				info.CPU, info.MemMB, info.MemLimitMB = containerLoad(asMap(raw))
			}
		}
		mu.Lock()
		out.Info[c.Name] = info
		mu.Unlock()
	})
	return out, nil
}

// containerLoad reads a stats sample: CPU in percent of one core, memory
// used without the page cache, and the limit.
func containerLoad(m map[string]any) (cpu, memMB, limitMB float64) {
	now, pre := asMap(m["cpu_stats"]), asMap(m["precpu_stats"])
	cpuDelta := asFloat(asMap(now["cpu_usage"])["total_usage"]) - asFloat(asMap(pre["cpu_usage"])["total_usage"])
	sysDelta := asFloat(now["system_cpu_usage"]) - asFloat(pre["system_cpu_usage"])
	cores := asFloat(now["online_cpus"])
	if sysDelta > 0 && cpuDelta > 0 {
		cpu = cpuDelta / sysDelta * max(cores, 1) * percent
	}
	mem := asMap(m["memory_stats"])
	used := asFloat(mem["usage"]) - asFloat(asMap(mem["stats"])["inactive_file"])
	limit := asFloat(mem["limit"])
	// Docker reports the host's memory as the limit of an unlimited container.
	if limit >= hostMemory {
		limit = 0
	}
	return cpu, max(used, 0) / bytesPerMB, limit / bytesPerMB
}

// hostMemory: a limit this large is none (Docker reports 2^63 or the host).
const hostMemory = 1 << 50

// logLines splits Docker's log stream into lines: without a TTY each
// frame has an 8-byte header (stream, 0, 0, 0, size).
func logLines(raw string) []string {
	var text strings.Builder
	b := []byte(raw)
	for len(b) >= dockerFrameHead && b[1] == 0 && b[2] == 0 && b[3] == 0 && b[0] <= 2 {
		size := int(b[4])<<24 | int(b[5])<<16 | int(b[6])<<8 | int(b[7])
		end := min(dockerFrameHead+size, len(b))
		text.Write(b[dockerFrameHead:end])
		b = b[end:]
	}
	text.Write(b)
	var out []string
	for _, line := range strings.Split(strings.TrimRight(text.String(), "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, MaskSecrets(line))
		}
	}
	return out
}

// dockerFrameHead is the size of a log frame's header.
const dockerFrameHead = 8

// DemoDockerDetail adds restarts, a memory kill and log lines to the demo
// containers.
func DemoDockerDetail() *DockerDetail {
	return &DockerDetail{Info: map[string]ContainerInfo{
		"gitea":         {CPU: 1.2, MemMB: 182, MemLimitMB: 1024},
		"immich-server": {CPU: 14.5, MemMB: 1630, MemLimitMB: 4096},
		"paperless": {CPU: 3.1, MemMB: 640, Restarts: 2, Logs: []string{
			"[2026-10-02 07:58:12] celery.worker: Connected to redis://broker:6379/0",
			"[2026-10-02 08:01:40] paperless.consumer: Waiting for redis… health check failed",
			"[2026-10-02 08:01:40] password=*** (masked)"}},
		"restic-nightly": {},
		"umami": {Restarts: 5, OOMKilled: true, Logs: []string{
			"Listening on port 3000",
			"FATAL ERROR: Reached heap limit Allocation failed - JavaScript heap out of memory"}},
	}}
}

func init() {
	Register(DockerDetailSource)
	Register(DockerData)
	Register(testOf{DockerData, func(d any) map[string]any { return map[string]any{"containers": len(d.(*DockerDataset).Containers)} }})
}
