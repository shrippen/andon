package sources

// Today's timesheets with names, description, tags and billable, for the
// Kimai Lite day list (edit, split, delete). Fetched only when it opens.
//
//	GET /api/timesheets?begin=<today>T00:00:00&full=true

import (
	"context"
	"net/url"
	"sort"
	"time"

	"andon/internal/enums"
)

const kimaiDayTTL = 30 * time.Second

// KimaiDay is one user's timesheets of today, oldest first; a running one
// has a zero End.
type KimaiDay struct {
	Sheets []KimaiTimer
}

type KimaiDaySource struct{}

func (KimaiDaySource) Key() string                { return "kimai.day" }
func (KimaiDaySource) TTL() time.Duration         { return kimaiDayTTL }
func (KimaiDaySource) Service() enums.ServiceType { return enums.ServiceKimai }

func (KimaiDaySource) Fetch(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return DemoKimaiDay(time.Now()), nil
	}
	api, err := kimaiAPI(sctx)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	sheets, err := api.Pages(ctx, "timesheets", url.Values{"begin": {midnight.Format(kimaiDateTime)}, "full": {"true"}})
	if err != nil {
		return nil, fetchError(err)
	}

	out := &KimaiDay{}
	for _, raw := range sheets {
		out.Sheets = append(out.Sheets, kimaiTimer(raw))
	}
	sort.Slice(out.Sheets, func(a, b int) bool { return out.Sheets[a].Begin.Before(out.Sheets[b].Begin) })
	return out, nil
}

// Sheet finds one of the day's timesheets; nil if it is not among them.
func (d *KimaiDay) Sheet(id int64) *KimaiTimer {
	for i := range d.Sheets {
		if d.Sheets[i].ID == id {
			return &d.Sheets[i]
		}
	}
	return nil
}

func init() {
	Register(KimaiDaySource{})
}
