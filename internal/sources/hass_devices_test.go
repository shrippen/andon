package sources

import "testing"

// Home Assistant's template answer maps battery sensors to devices; a
// sensor without device (null) keeps none.
func TestHassSetDevices(t *testing.T) {
	d := &HassDataset{Entities: []Entity{{ID: "sensor.a_battery"}, {ID: "sensor.b_battery"}, {ID: "light.x"}}}
	d.setDevices([]any{[]any{"sensor.a_battery", "dev1"}, []any{"sensor.b_battery", nil}})
	if d.Entities[0].Device != "dev1" || d.Entities[1].Device != "" || d.Entities[2].Device != "" {
		t.Fatalf("devices: %+v", d.Entities)
	}
}
