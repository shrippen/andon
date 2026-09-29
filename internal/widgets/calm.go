package widgets

// Calm: a tile has nothing to do. Each type that can tell says so from
// its finished view; with the frame option "only_issues" the board then
// hides the tile until something needs attention.

import (
	"reflect"

	"andon/internal/sources"
)

// calmChecks read "nothing to do" from a type's view.
var calmChecks = map[string]func(v map[string]any) bool{
	"backups": func(v map[string]any) bool {
		lines, _ := v["Rows"].([]BackupLine)
		for _, l := range lines {
			if l.State != "ok" {
				return false
			}
		}
		return v["Total"] != 0 && v["Total"] != nil
	},
	"monitors":    func(v map[string]any) bool { return v["Total"] != nil && v["Up"] == v["Total"] },
	"disks":       func(v map[string]any) bool { return v["Total"] != nil && v["Healthy"] == v["Total"] },
	"conn_health": func(v map[string]any) bool { return v["Total"] != nil && v["Healthy"] == v["Total"] },
	// Stacks stopped on purpose (Resting) count as fine.
	"komodo_stacks": func(v map[string]any) bool {
		running, _ := v["Running"].(int)
		resting, _ := v["Resting"].(int)
		return v["Stacks"] != nil && running+resting == v["Stacks"] && v["Updates"] == 0 && v["Alerts"] == 0
	},
	"truenas_pools":    func(v map[string]any) bool { return v["Pools"] != nil && v["Alerts"] == 0 },
	"expiry":           func(v map[string]any) bool { return v["Total"] == 0 },
	"unbilled_age":     func(v map[string]any) bool { return isZero(v["Total"]) },
	"invoice_aging":    func(v map[string]any) bool { return v["Count"] == 0 },
	"status_light":     func(v map[string]any) bool { return v["State"] == "green" },
	"receipts_missing": func(v map[string]any) bool { return v["Count"] == 0 && v["Setup"] == false },
	"exposure":         func(v map[string]any) bool { return v["Total"] != nil && v["Total"] != 0 && v["Open"] == 0 },
	"deadlines":        func(v map[string]any) bool { return v["Configured"] == true && lenOf(v["Items"]) == 0 },
	"today":            func(v map[string]any) bool { return lenOf(v["Items"]) == 0 },
	"month_close": func(v map[string]any) bool {
		steps, _ := v["Steps"].([]CloseStep)
		return len(steps) > 0 && v["Done"] == len(steps)
	},
	"uptime_month": func(v map[string]any) bool {
		rows, _ := v["Rows"].([]UptimeRow)
		for _, r := range rows {
			if r.Tier != "ok" {
				return false
			}
		}
		return len(rows) > 0
	},
	// Count is the inbox, or the tag's documents when a tag is set.
	"paperless_inbox": func(v map[string]any) bool {
		_, ok := v["Data"].(*sources.PaperlessDataset)
		return ok && v["Count"] == 0
	},
	// Hint lists (ExtraHints): calm without hints.
	"hints":   func(v map[string]any) bool { return v["Hints"] != nil && lenOf(v["Hints"]) == 0 },
	"updates": func(v map[string]any) bool { return v["Hints"] != nil && lenOf(v["Hints"]) == 0 },
}

func init() {
	for key := range calmChecks {
		calmType(key)
	}
}

// IsCalm tells whether a type's view reports nothing to do.
func IsCalm(key string, view map[string]any) bool {
	check, ok := calmChecks[key]
	return ok && view != nil && check(view)
}

func isZero(v any) bool {
	switch n := v.(type) {
	case int:
		return n == 0
	case float64:
		return n == 0
	}
	return false
}

// lenOf is the length of a slice in a view, whatever its element type.
func lenOf(v any) int {
	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Slice {
		return rv.Len()
	}
	return 0
}
