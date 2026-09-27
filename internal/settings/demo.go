//go:build !release

package settings

// demoMode reads ANDON_DEMO: demo accounts and data for screenshots.
func demoMode() bool { return envBool("ANDON_DEMO", false) }
