package util

import "strings"

// LocalPath keeps a redirect target on this host, else "/": no open
// redirect through "//evil" or "/\evil", which browsers read as hosts.
func LocalPath(target string) string {
	if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") || strings.Contains(target, "\\") {
		return "/"
	}
	return target
}
