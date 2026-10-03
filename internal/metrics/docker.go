package metrics

import "andon/internal/sources"

// ContainerCrashed: restarting in a loop, or stopped with an error code.
// A job that ended with 0 (backup, migration) did its work.
func ContainerCrashed(c sources.Container) bool {
	return c.State == sources.StateRestarting || (c.State == sources.StateExited && c.ExitCode != 0)
}

// ContainerUnhealthy: a failing health check; the container runs but
// does not work.
func ContainerUnhealthy(c sources.Container) bool { return c.Health == sources.HealthUnhealthy }
