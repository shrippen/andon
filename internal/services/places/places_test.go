package places_test

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"andon/internal/drivers/httpclient"
	"andon/internal/enums"
	"andon/internal/services/places"
)

// denyAll blocks every outbound request and counts the attempts.
func denyAll(t *testing.T) *atomic.Int32 {
	t.Helper()
	var asked atomic.Int32
	httpclient.SetGuard(func(string, []net.IP) bool {
		asked.Add(1)
		return false
	})
	t.Cleanup(func() { httpclient.SetGuard(nil) })
	return &asked
}

// Names shorter than two characters (counted in runes, after trimming)
// give nothing and never reach the geocoder.
func TestSearchIgnoresShortNames(t *testing.T) {
	asked := denyAll(t)

	for _, name := range []string{"", "  ", "W", " Ö "} {
		got, err := places.Search(context.Background(), name, enums.LocaleDE)
		if err != nil || got != nil {
			t.Fatalf("%q: got %v, %v", name, got, err)
		}
	}
	if n := asked.Load(); n != 0 {
		t.Fatalf("geocoder asked %d times", n)
	}
}

// A real name is sent to the geocoder; blocked egress surfaces as an
// error instead of an empty hit list.
func TestSearchReportsUnreachableGeocoder(t *testing.T) {
	denyAll(t)

	got, err := places.Search(context.Background(), "Weimar", enums.LocaleDE)
	if err == nil {
		t.Fatalf("expected error, got %v", got)
	}
}
