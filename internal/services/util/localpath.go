package util

import "andon/internal/weburl"

// LocalPath keeps a redirect target on this host, else "/": no open
// redirect through "//evil" or "/\evil", which browsers read as hosts.
func LocalPath(target string) string {
	if !weburl.IsLocalPath(target) {
		return "/"
	}
	return target
}
