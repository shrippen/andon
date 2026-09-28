package rules

import (
	"andon/internal/enums"
	"andon/internal/sources"
)

func init() {
	svc := string(enums.ServiceDocker)

	// A health check failing: the container runs but does not work.
	Register("docker.unhealthy", svc, nil, func(raw any, _ map[string]any, _ Env) []Finding {
		data, _ := raw.(*sources.DockerDataset)
		var found []Finding
		for _, c := range data.Containers {
			if c.Health == sources.HealthUnhealthy {
				found = append(found, svcFinding(svc, "docker.unhealthy", "unhealthy:"+c.Name, "docker.unhealthy",
					enums.SeverityWarn, "", map[string]any{"name": c.Name, "status": c.Status}))
			}
		}
		return found
	})

	// Restarting in a loop, or stopped with an error code. A job that
	// ended with 0 (backup, migration) did its work.
	Register("docker.crashed", svc, nil, func(raw any, _ map[string]any, _ Env) []Finding {
		data, _ := raw.(*sources.DockerDataset)
		var found []Finding
		for _, c := range data.Containers {
			crashed := c.State == sources.StateRestarting || (c.State == sources.StateExited && c.ExitCode != 0)
			if crashed {
				found = append(found, svcFinding(svc, "docker.crashed", "crashed:"+c.Name, "docker.crashed",
					enums.SeverityCritical, "", map[string]any{"name": c.Name, "status": c.Status}))
			}
		}
		return found
	})
}
