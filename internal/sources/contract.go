package sources

import (
	"context"
	"time"

	"andon/internal/drivers/services"
)

// WorkContract is the working time a user keeps in Kimai (user settings →
// working time), the only source of daily and weekly targets:
//
//	work_monday = 28800 s … work_sunday = 0  →  Mon 8 h … Sun free
type WorkContract struct {
	Day [7]int // minutes per weekday, index 0 = Monday
}

// contractPrefs are Kimai's preference names, Monday first.
var contractPrefs = [7]string{"work_monday", "work_tuesday", "work_wednesday", "work_thursday", "work_friday", "work_saturday", "work_sunday"}

// WeekMinutes is the weekly target.
func (c *WorkContract) WeekMinutes() int {
	if c == nil {
		return 0
	}
	sum := 0
	for _, m := range c.Day {
		sum += m
	}
	return sum
}

// Minutes is the target of one day's weekday (0 on days off).
func (c *WorkContract) Minutes(d time.Time) int {
	if c == nil {
		return 0
	}
	return c.Day[(int(d.Weekday())+6)%7]
}

// parseContract reads the working time from /api/users/me; nil when the
// user has none (all days zero or unset).
func parseContract(me any) *WorkContract {
	values := map[string]float64{}
	for _, raw := range asList(asMap(me)["preferences"]) {
		p := asMap(raw)
		values[asStr(p["name"])] = asFloat(p["value"])
	}
	c := &WorkContract{}
	for i, name := range contractPrefs {
		c.Day[i] = int(values[name]) / secondsPerMinute
	}
	if c.WeekMinutes() == 0 {
		return nil
	}
	return c
}

// loadContract asks Kimai for the user's working time; nil on any error
// (older Kimai, missing permission): no targets then.
func loadContract(ctx context.Context, api services.KimaiApi) *WorkContract {
	me, err := api.Get(ctx, "users/me", nil)
	if err != nil {
		return nil
	}
	return parseContract(me)
}
