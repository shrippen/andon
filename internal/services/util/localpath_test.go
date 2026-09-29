package util

import "testing"

// TestLocalPath: only paths on this host; "//evil" and "/\evil" are
// read by browsers as another host.
func TestLocalPath(t *testing.T) {
	cases := map[string]string{"/boards/1": "/boards/1", "//evil.com": "/", "/\\evil.com": "/", "https://evil.com": "/", "": "/",
		"/me?x=1": "/me?x=1"}
	for in, want := range cases {
		if got := LocalPath(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}
