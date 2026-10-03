package sources

// Calendar as a connection, so rules can compare appointments with other
// data (e.g. Kimai bookings). The feed address usually contains a secret,
// so it is taken from the token when one is stored:
//
//	token  https://cloud.example/remote.php/dav/…?export  (encrypted)
//	url    any address of the calendar, for the tile

import (
	"context"
	"time"

	"andon/internal/drivers/httpclient"
	"andon/internal/enums"
	"andon/internal/sources/demoworld"
)

// calendarDays is the window before and after today.
const calendarDays = 30

var CalendarData = source{key: "calendar.data", ttl: icalTTL, service: enums.ServiceCalendar, fetch: fetchCalendarData}

func fetchCalendarData(ctx context.Context, sctx Ctx) (any, error) {
	now := time.Now()
	if isDemo(sctx) {
		return DemoCalendar(now), nil
	}
	feed := sctx.URL
	if sctx.Secret != "" {
		feed = sctx.Secret
	}
	text, err := httpclient.GetText(ctx, feed, httpclient.Options{SkipVerify: !sctx.VerifyTLS})
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}
	return &CalendarResult{Events: Occurrences(text, now.AddDate(0, 0, -calendarDays), now.AddDate(0, 0, calendarDays))}, nil
}

// DemoCalendar has a customer appointment two days ago, a call today and
// a private one in three days.
func DemoCalendar(now time.Time) *CalendarResult {
	data := &CalendarResult{}
	demoworld.MustDecode("calendar", now, data)
	return data
}

func init() {
	Register(CalendarData)
	Register(testOf{CalendarData, func(d any) map[string]any { return map[string]any{"events": len(d.(*CalendarResult).Events)} }})
}
