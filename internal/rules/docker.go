package rules

import (
	"andon/internal/enums"
	"andon/internal/sources"
)

func init() {
	// A health check failing: the container runs but does not work.
	registerTyped("docker.unhealthy", enums.ServiceDocker, noSettings{}, dockerUnhealthy)

	// Restarting in a loop, or stopped with an error code. A job that
	// ended with 0 (backup, migration) did its work.
	registerTyped("docker.crashed", enums.ServiceDocker, noSettings{}, dockerCrashed)
}

func dockerUnhealthy(data *sources.DockerDataset, _ noSettings, _ Env) []Finding {
	var found []Finding
	for _, c := range data.Containers {
		if c.Health == sources.HealthUnhealthy {
			found = append(found, svcFinding(dockerSvc, "docker.unhealthy", "unhealthy:"+c.Name, "docker.unhealthy",
				enums.SeverityWarn, "", map[string]any{"name": c.Name, "status": c.Status}))
		}
	}
	return found
}

func dockerCrashed(data *sources.DockerDataset, _ noSettings, _ Env) []Finding {
	var found []Finding
	for _, c := range data.Containers {
		crashed := c.State == sources.StateRestarting || (c.State == sources.StateExited && c.ExitCode != 0)
		if crashed {
			found = append(found, svcFinding(dockerSvc, "docker.crashed", "crashed:"+c.Name, "docker.crashed",
				enums.SeverityCritical, "", map[string]any{"name": c.Name, "status": c.Status}))
		}
	}
	return found
}
