package widgets

import (
	"strings"
	"time"
)

// Check keys: config values a type cannot use, refused on save.
const (
	CheckBadWindow   = "widget.bad_window"
	CheckBadTimezone = "widget.bad_timezone"
)

// checks read what a type's decoder would silently drop: a window
// "25-3" would mean "always", a zone "Europe/Berln" UTC.
var checks = map[string]func(raw map[string]any) string{
	"update_window": func(raw map[string]any) string {
		if w := strings.TrimSpace(asString(raw["window"])); w != "" {
			if _, _, ok := parseSpan(w); !ok {
				return CheckBadWindow
			}
		}
		return checkZone(raw)
	},
	"today":    checkZone,
	"greeting": checkZone,
}

// checkZone: an empty zone means the default, any other must be known.
func checkZone(raw map[string]any) string {
	zone := strings.TrimSpace(asString(raw["timezone"]))
	if zone == "" {
		return ""
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return CheckBadTimezone
	}
	return ""
}

// Check reports the first config value a type cannot use as a catalog
// key, "" when all are fine.
func Check(key string, config map[string]any) string {
	check := registry[key].Check
	if check == nil {
		check = checks[key]
	}
	if check == nil {
		return ""
	}
	return check(config)
}
