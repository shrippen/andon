package sources

import (
	"strings"
	"testing"
)

// An automatic icon tries Dashboard Icons and selfh.st by the link's
// title, then the site's favicon.
func TestAutoIconCandidates(t *testing.T) {
	got := IconCandidates("auto:uptime-kuma", "https://status.lan/dash")
	if len(got) != 3 || !strings.Contains(got[0], "dashboard-icons/svg/uptime-kuma.svg") ||
		!strings.Contains(got[1], "selfhst/icons/svg/uptime-kuma.svg") || got[2] != "https://status.lan/favicon.ico" {
		t.Fatalf("candidates: %v", got)
	}
}
