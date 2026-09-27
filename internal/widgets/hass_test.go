package widgets_test

import (
	"testing"

	"andon/internal/enums"
	"andon/internal/sources"
	"andon/internal/widgets"
)

// TestHassLowBattery: the unit comes apart from the value, and a battery
// below 15 % is flagged.
func TestHassLowBattery(t *testing.T) {
	kind, _ := widgets.Get("hass")
	cfg, _ := widgets.Decode("hass", map[string]any{"entities": []any{"sensor.bad_battery", "sensor.temp"}})
	data := &sources.HassDataset{Entities: []sources.Entity{
		{ID: "sensor.bad_battery", Name: "Fenster Bad", Domain: "sensor", State: "8", Unit: "%", DeviceClass: "battery"},
		{ID: "sensor.temp", Name: "Wohnzimmer", Domain: "sensor", State: "21.4", Unit: "°C", DeviceClass: "temperature"},
	}}
	rows := kind.View(cfg, map[string]any{"data": data}, ctxFor(enums.ServiceHomeAssistant, nil))["Rows"].([]widgets.HassRow)
	if len(rows) != 2 || !rows[0].Low || rows[0].Value != "8" || rows[0].Unit != "%" || rows[1].Low {
		t.Fatalf("rows: %+v", rows)
	}
}
