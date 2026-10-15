package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The notes between two versions, oldest first: from excluded, to's
// releases included, pre-releases left out.
func TestReleaseNotes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[{"tag_name": "v2.3.0", "body": "next"}, {"tag_name": "v2.2.1", "body": "fix"},
			{"tag_name": "v2.2.1-rc1", "prerelease": true}, {"tag_name": "v2.2.0", "body": "feature"}, {"tag_name": "v2.1.4", "body": "old"}]`))
	}))
	defer srv.Close()
	saved := githubAPI
	githubAPI = srv.URL
	t.Cleanup(func() { githubAPI = saved })

	got, err := ReleaseNotes(context.Background(), "immich-app/immich", "v2.1", "v2.2")
	if err != nil || len(got) != 2 || got[0].Tag != "v2.2.0" || got[1].Body != "fix" {
		t.Fatalf("notes: %+v %v", got, err)
	}
}
