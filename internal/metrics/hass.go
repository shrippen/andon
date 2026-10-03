package metrics

import (
	"slices"
	"strconv"

	"andon/internal/sources"
)

// BatteryLevel is a battery sensor's charge in percent; ok is false for
// any other entity.
func BatteryLevel(e sources.Entity) (float64, bool) {
	level, err := strconv.ParseFloat(e.State, 64)
	if e.DeviceClass != "battery" || e.Unit != "%" || err != nil {
		return 0, false
	}
	return level, true
}

// Battery is one battery below the warning level, named by its first
// sensor.
type Battery struct {
	Entity sources.Entity
	Level  float64
}

// LowBatteries lists batteries under warn. Sensors of one device
// ("Batterie", "Batterie+") are one battery at their lowest level, named
// by the sensor first in id order so its hint stays the same.
func LowBatteries(data *sources.HassDataset, warn float64) []Battery {
	var out []Battery
	byDevice := map[string]int{}
	for _, e := range data.Entities {
		level, ok := BatteryLevel(e)
		if !ok {
			continue
		}
		i, seen := byDevice[e.Device]
		if e.Device == "" || !seen {
			if e.Device != "" {
				byDevice[e.Device] = len(out)
			}
			out = append(out, Battery{e, level})
			continue
		}
		out[i].Level = min(out[i].Level, level)
	}
	return slices.DeleteFunc(out, func(b Battery) bool { return b.Level >= warn })
}
