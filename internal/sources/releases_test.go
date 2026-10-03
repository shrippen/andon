package sources

import (
	"encoding/json"
	"testing"
)

// TestReleases: drafts are skipped, security notes flagged, versions found
// whatever the tag's prefix.
func TestReleases(t *testing.T) {
	var body any
	_ = json.Unmarshal([]byte(`[
		{"tag_name": "version/2025.8.3", "html_url": "https://github.com/goauthentik/authentik/releases/tag/version%2F2025.8.3", "published_at": "2026-09-20T10:00:00Z", "body": "Fixes CVE-2026-1234"},
		{"tag_name": "version/2025.8.4", "draft": true},
		{"tag_name": "version/2025.8.2", "published_at": "2026-09-01T10:00:00Z", "body": "Bug fixes"}]`), &body)
	r := &ReleasesResult{ByRepo: map[string][]Release{"goauthentik/authentik": parseReleases("goauthentik/authentik", body)}}
	if n := len(r.ByRepo["goauthentik/authentik"]); n != 2 {
		t.Fatalf("releases: %d", n)
	}
	rel, ok := r.FindRelease("goauthentik/authentik", "2025.8.3")
	if !ok || !rel.Security || rel.Published.Day() != 20 {
		t.Fatalf("found: %+v %v", rel, ok)
	}
	if rel, ok := r.FindRelease("goauthentik/authentik", "v2025.8.2"); !ok || rel.Security {
		t.Fatalf("plain: %+v %v", rel, ok)
	}
	if _, ok := r.FindRelease("goauthentik/authentik", "2024.1"); ok {
		t.Fatal("unknown version found")
	}
}
