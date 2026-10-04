package widgets

import (
	"strings"
	"time"
)

// Check keys: config values a type cannot use, refused on save.
const (
	CheckBadWindow   = "widget.bad_window"
	CheckBadTimezone = "widget.bad_timezone"
	CheckRequired    = "widget.field_required"
)

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
// key, "" when all are fine. A type's Check reads what its decoder would
// silently drop: a zone "Europe/Berln" would mean UTC.
func Check(key string, config map[string]any) string {
	for _, f := range registry[key].Fields {
		if f.Required && blank(config[f.Key]) {
			return CheckRequired
		}
	}
	check := registry[key].Check
	if check == nil {
		return ""
	}
	return check(config)
}

// blank: a value a required field cannot stand for: nothing, spaces or
// an empty list.
func blank(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(t) == ""
	case []any:
		return len(t) == 0
	case []string:
		return len(t) == 0
	}
	return false
}
