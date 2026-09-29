package weburl

import "testing"

func TestKinds(t *testing.T) {
	for s, want := range map[string][2]bool{
		"https://a": {true, false}, "HTTP://a": {true, false}, "javascript:x": {false, false}, "/boards/2": {false, true},
		"//evil": {false, false}, "/\\evil": {false, false}, "ftp://a": {false, false},
	} {
		if got := [2]bool{IsWeb(s), IsLocalPath(s)}; got != want {
			t.Errorf("%q: %v, want %v", s, got, want)
		}
	}
}
