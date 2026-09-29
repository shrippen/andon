// Package weburl answers what kind of address a string is, the same way
// for every layer (sources, services, widgets).
package weburl

import "strings"

// IsWeb reports an absolute http(s) address: "https://git.lan/x".
func IsWeb(s string) bool {
	lower := strings.ToLower(s)
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://")
}

// IsLocalPath reports a path on this host: "/boards/2", not "//evil"
// or "/\evil", which browsers read as another host.
func IsLocalPath(s string) bool {
	return strings.HasPrefix(s, "/") && !strings.HasPrefix(s, "//") && !strings.Contains(s, "\\")
}
