package spaces

import (
	"slices"
	"strconv"
	"strings"
	"time"
)

// Maintenance is a planned work window of a space, e.g. a Proxmox
// update: hints of the listed connections (all when none are listed)
// keep coming but are not pushed and carry a mark until the window ends.
type Maintenance struct {
	Until       time.Time
	Connections []int64
	Note        string
}

const (
	maintenanceKey = "maintenance"
	// formTime is what an <input type="datetime-local"> sends.
	formTime = "2006-01-02T15:04"
)

// MaintenanceOf reads a space's window from its settings.
func MaintenanceOf(settings map[string]any) Maintenance {
	raw, _ := settings[maintenanceKey].(map[string]any)
	var m Maintenance
	if until, ok := raw["until"].(string); ok {
		m.Until, _ = time.Parse(time.RFC3339, until)
	}
	m.Note, _ = raw["note"].(string)
	list, _ := raw["connections"].([]any)
	for _, v := range list {
		if id, ok := v.(float64); ok {
			m.Connections = append(m.Connections, int64(id))
		}
	}
	return m
}

// Active: the window has not ended yet.
func (m Maintenance) Active(now time.Time) bool {
	return now.Before(m.Until)
}

// Covers: a hint of connection connID (nil for hints across services)
// falls in the active window.
func (m Maintenance) Covers(connID *int64, now time.Time) bool {
	if !m.Active(now) {
		return false
	}
	if len(m.Connections) == 0 {
		return true
	}
	return connID != nil && slices.Contains(m.Connections, *connID)
}

// UntilInput is the end in the form's local-time format, "" if none.
func (m Maintenance) UntilInput(loc *time.Location) string {
	if m.Until.IsZero() {
		return ""
	}
	return m.Until.In(loc).Format(formTime)
}

// ParseMaintenance reads the settings form: end in local time (loc), the
// checked connection ids, a note. An empty end clears the window.
func ParseMaintenance(get func(string) string, connIDs []string, loc *time.Location) map[string]any {
	out := map[string]any{"until": "", "connections": []any{}, "note": strings.TrimSpace(get("maint_note"))}
	until, err := time.ParseInLocation(formTime, strings.TrimSpace(get("maint_until")), loc)
	if err != nil {
		return out
	}
	out["until"] = until.UTC().Format(time.RFC3339)
	ids := []any{}
	for _, raw := range connIDs {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			ids = append(ids, float64(id))
		}
	}
	out["connections"] = ids
	return out
}
