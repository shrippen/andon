package sources_test

import (
	"strings"
	"testing"
	"time"

	"andon/internal/sources"
)

const icalText = "BEGIN:VCALENDAR\r\n" +
	"BEGIN:VEVENT\r\nUID:standup\r\nDTSTART;TZID=Europe/Berlin:20260928T090000\r\n" +
	"RRULE:FREQ=WEEKLY;BYDAY=MO,WE;COUNT=4\r\nEXDATE;TZID=Europe/Berlin:20260930T090000\r\n" +
	"SUMMARY:Stand\r\n up\r\nEND:VEVENT\r\n" +
	// The 5 October instance moved to 10:00.
	"BEGIN:VEVENT\r\nUID:standup\r\nRECURRENCE-ID;TZID=Europe/Berlin:20261005T090000\r\n" +
	"DTSTART;TZID=Europe/Berlin:20261005T100000\r\nSUMMARY:Standup (später)\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:holiday\r\nDTSTART;VALUE=DATE:20261003\r\nSUMMARY:Tag der Einheit\\, frei\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:gone\r\nDTSTART:20261001T080000Z\r\nSTATUS:CANCELLED\r\nSUMMARY:Gone\r\nEND:VEVENT\r\n" +
	"BEGIN:VEVENT\r\nUID:monthly\r\nDTSTART:20260131T120000Z\r\nRRULE:FREQ=MONTHLY;UNTIL=20261231T000000Z\r\nSUMMARY:Ultimo\r\nEND:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func TestICalOccurrences(t *testing.T) {
	from := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	var got []string
	for _, e := range sources.Occurrences(icalText, from, to) {
		got = append(got, e.Start.UTC().Format("01-02T15:04")+" "+e.Title)
	}
	// Wed 30.09. is excluded, the 05.10. instance moved to 10:00, the
	// cancelled event and months without a 31st are skipped.
	want := []string{
		"09-28T07:00 Standup",
		"10-02T22:00 Tag der Einheit, frei",
		"10-05T08:00 Standup (später)",
		"10-07T07:00 Standup",
	}
	if joined := strings.Join(got, "\n"); joined != strings.Join(want, "\n") {
		t.Fatalf("occurrences:\n%s", joined)
	}
}

func TestWithLogin(t *testing.T) {
	if got := sources.WithLogin("https://h/x?a=1", "anna", "p@ss"); got != "https://anna:p%40ss@h/x?a=1" {
		t.Errorf("login: %q", got)
	}
	if got := sources.WithLogin("https://h/x", "", "pw"); got != "https://h/x" {
		t.Errorf("no user: %q", got)
	}
}

// TestFeedURL: calendar apps hand out webcal:// links (Apple, Outlook);
// they are plain HTTPS.
func TestFeedURL(t *testing.T) {
	cases := map[string]string{
		"webcal://h/x.ics":   "https://h/x.ics",
		"WEBCALS://h/x.ics":  "https://h/x.ics",
		"https://h/x.ics":    "https://h/x.ics",
		"http://lan/cal.ics": "http://lan/cal.ics",
	}
	for in, want := range cases {
		if got := sources.FeedURL(in); got != want {
			t.Errorf("%s → %q, want %q", in, got, want)
		}
	}
}

// TestICalRulesByDay: monthly and yearly rules on a weekday of the month
// or a day of the month, as Outlook and Google write them.
func TestICalRulesByDay(t *testing.T) {
	text := "BEGIN:VCALENDAR\r\n" +
		// First Monday, last Friday, 15th and last day of each month.
		"BEGIN:VEVENT\r\nUID:a\r\nDTSTART:20260105T090000Z\r\nRRULE:FREQ=MONTHLY;BYDAY=1MO\r\nSUMMARY:first-mo\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:b\r\nDTSTART:20260130T090000Z\r\nRRULE:FREQ=MONTHLY;BYDAY=-1FR\r\nSUMMARY:last-fr\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:c\r\nDTSTART:20260115T090000Z\r\nRRULE:FREQ=MONTHLY;BYMONTHDAY=15,-1\r\nSUMMARY:mday\r\nEND:VEVENT\r\n" +
		// Second Sunday in May (Mother's day).
		"BEGIN:VEVENT\r\nUID:d\r\nDTSTART;VALUE=DATE:20250511\r\nRRULE:FREQ=YEARLY;BYMONTH=5;BYDAY=2SU\r\nSUMMARY:mum\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"
	from := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	var got []string
	for _, e := range sources.Occurrences(text, from, to) {
		got = append(got, e.Start.Format("01-02")+" "+e.Title)
	}
	want := []string{
		"04-06 first-mo", "04-15 mday", "04-24 last-fr", "04-30 mday",
		"05-04 first-mo", "05-10 mum", "05-15 mday", "05-29 last-fr", "05-31 mday",
	}
	if joined := strings.Join(got, "\n"); joined != strings.Join(want, "\n") {
		t.Fatalf("occurrences:\n%s", joined)
	}
}

// TestICalOngoing: an event that began before the window and still runs
// is listed, e.g. a holiday week; one that ended is not.
func TestICalOngoing(t *testing.T) {
	text := "BEGIN:VCALENDAR\r\n" +
		"BEGIN:VEVENT\r\nUID:a\r\nDTSTART;VALUE=DATE:20261001\r\nDTEND;VALUE=DATE:20261010\r\nSUMMARY:holiday\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:b\r\nDTSTART:20261004T080000Z\r\nDURATION:PT3H\r\nSUMMARY:workshop\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:c\r\nDTSTART;VALUE=DATE:20261003\r\nSUMMARY:yesterday\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:d\r\nDTSTART:20261004T060000Z\r\nDTEND:20261004T070000Z\r\nSUMMARY:done\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"
	from := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	var got []string
	for _, e := range sources.Occurrences(text, from, from.AddDate(0, 0, 7)) {
		got = append(got, e.Title+" "+e.End.UTC().Format("01-02T15:04"))
	}
	if joined := strings.Join(got, ","); joined != "holiday 10-09T22:00,workshop 10-04T11:00" {
		t.Fatalf("got %s", joined)
	}
}

// TestICalWindowsZone: Outlook names zones the Windows way; old
// Thunderbird prefixes the IANA name.
func TestICalWindowsZone(t *testing.T) {
	text := "BEGIN:VCALENDAR\r\n" +
		"BEGIN:VEVENT\r\nUID:a\r\nDTSTART;TZID=Pacific Standard Time:20261005T090000\r\nSUMMARY:pst\r\nEND:VEVENT\r\n" +
		"BEGIN:VEVENT\r\nUID:b\r\nDTSTART;TZID=/mozilla.org/20050126_1/America/New_York:20261005T090000\r\nSUMMARY:ny\r\nEND:VEVENT\r\n" +
		"END:VCALENDAR\r\n"
	from := time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	var got []string
	for _, e := range sources.Occurrences(text, from, from.AddDate(0, 0, 2)) {
		got = append(got, e.Start.UTC().Format("15:04")+" "+e.Title)
	}
	if joined := strings.Join(got, ","); joined != "13:00 ny,16:00 pst" {
		t.Fatalf("got %s", joined)
	}
}
