//go:build release

package settings

// demoMode is always off in release builds.
func demoMode() bool { return false }
