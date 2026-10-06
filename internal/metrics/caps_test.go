package metrics_test

import (
	"testing"

	"andon/internal/caps"
	"andon/internal/enums"
	"andon/internal/metrics"
)

// The mileage plugin takes exactly the rides that earn the km rate.
func TestPluginTakesPayableRides(t *testing.T) {
	plugin := caps.Full(caps.HolderOf(enums.ServiceKimai))
	for _, mode := range []string{metrics.ModeDriving, metrics.ModeMotorcycle, metrics.ModeCycling, metrics.ModeWalking, metrics.ModeRunning, metrics.ModeUnknown} {
		payable := metrics.Payable(metrics.Ride{Mode: mode})
		if takes := plugin.Can(caps.Rides, caps.Update, mode); takes != payable {
			t.Errorf("%s: plugin %v, payable %v", mode, takes, payable)
		}
	}
}
