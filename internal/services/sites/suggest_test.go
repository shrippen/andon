package sites_test

import (
	"context"
	"testing"

	"andon/internal/enums"
	"andon/internal/services/sites"
	"andon/internal/testkit"
)

// A new place's name comes from Dawarich's geocoder.
func TestSuggestName(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	geo := fakeDawarich(&recorder{}, map[string]string{"/api/v1/places/nearby": `{"places": [{"name": "Seeblick"}]}`})
	defer geo.Close()
	conn := testkit.Conn(t, d, who, space, enums.ServiceDawarich, geo.URL)

	name, err := sites.SuggestName(context.Background(), d, who, conn, 52.45, 13.2)
	if err != nil || name != "Seeblick" {
		t.Fatalf("name: %q, %v", name, err)
	}
}
