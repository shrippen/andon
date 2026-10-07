package sources

// iCal calendar feed (Nextcloud, Google, Outlook "secret address"):
// upcoming events within a window, recurring events expanded.
//
//	BEGIN:VEVENT
//	DTSTART;TZID=Europe/Berlin:20260928T090000   ─┐
//	RRULE:FREQ=WEEKLY;BYDAY=MO,WE;COUNT=10        ├─► Event{Start, Title}
//	SUMMARY:Standup                               ─┘   per occurrence
//
// Supported rules: FREQ DAILY/WEEKLY/MONTHLY/YEARLY with INTERVAL, COUNT,
// UNTIL, BYDAY (weekly: MO,WE; monthly/yearly: 1MO, -1FR), BYMONTHDAY
// (15, -1), BYMONTH (yearly), EXDATE and moved instances (RECURRENCE-ID).
// Other BY* parts are ignored. An event still running at the window's
// start (DTEND, DURATION) is listed too, e.g. a holiday week.

import (
	"bufio"
	"context"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"andon/internal/drivers/httpclient"
)

const (
	icalTTL       = 15 * time.Minute
	icalZone      = "Europe/Berlin" // for floating times
	icalDate      = "20060102"
	icalDateTime  = "20060102T150405"
	icalMaxEvents = 50
	icalMaxSteps  = 20000 // guards endless rules
	daysPerWeek   = 7
	hoursPerDay   = 24
)

// Event is one occurrence.
type Event struct {
	Start    time.Time
	End      time.Time // exclusive; zero for a timed event without length
	AllDay   bool
	Title    string
	Location string
}

// CalendarResult lists occurrences, soonest first.
type CalendarResult struct{ Events []Event }

// webcalSchemes are calendar-app links to plain HTTPS feeds.
var webcalSchemes = []string{"webcal://", "webcals://"}

// feedURL makes a calendar link fetchable: "webcal://h/x" → "https://h/x".
func feedURL(feed string) string {
	for _, scheme := range webcalSchemes {
		if len(feed) >= len(scheme) && strings.EqualFold(feed[:len(scheme)], scheme) {
			return "https://" + feed[len(scheme):]
		}
	}

	return feed
}

// WithLogin puts user and password into a feed address as Basic-Auth
// login, percent-encoded: "https://h/x" → "https://anna:p%40ss@h/x".
// Without a user, or on an unparsable address, the address stays.
func WithLogin(feed, user, password string) string {
	u, err := url.Parse(feed)
	if user == "" || err != nil {
		return feed
	}
	u.User = url.UserPassword(user, password)

	return u.String()
}

var CalendarSource = source{key: "ical", ttl: icalTTL, fetch: fetchCalendarSource, demo: true}

func fetchCalendarSource(ctx context.Context, sctx Ctx) (any, error) {
	if isDemo(sctx) {
		return demoCalendar(time.Now()), nil
	}
	text, err := httpclient.GetText(ctx, feedURL(asStr(sctx.Params["url"])), httpclient.Options{})
	if err != nil {
		return nil, newSourceError("%s", err.Error())
	}
	// "back" reaches into the past (the dialog's unbooked appointments).
	days, back := int(asFloat(sctx.Params["days"])), int(asFloat(sctx.Params["back"]))
	now := time.Now()
	return &CalendarResult{Events: Occurrences(text, now.AddDate(0, 0, -back), now.AddDate(0, 0, days))}, nil
}

// vevent is one parsed VEVENT: property name → value, params.
type vevent map[string]icalProp

type icalProp struct {
	Value  string
	Params map[string]string
}

// Occurrences returns the events of an iCal text starting in [from, to).
func Occurrences(text string, from, to time.Time) []Event {
	events := parseEvents(text)

	// Moved or cancelled instances replace their rule occurrence.
	replaced := map[string]map[int64]bool{}
	for _, ev := range events {
		rid, ok := ev["RECURRENCE-ID"]
		if !ok {
			continue
		}
		start, _, ok := icalTime(rid)
		if !ok {
			continue
		}
		uid := ev["UID"].Value
		if replaced[uid] == nil {
			replaced[uid] = map[int64]bool{}
		}
		replaced[uid][start.Unix()] = true
	}

	var out []Event
	for _, ev := range events {
		if strings.EqualFold(ev["STATUS"].Value, "CANCELLED") {
			continue
		}
		start, allDay, ok := icalTime(ev["DTSTART"])
		if !ok {
			continue
		}
		skip := exdates(ev)
		for k := range replaced[ev["UID"].Value] {
			if _, isOverride := ev["RECURRENCE-ID"]; !isOverride {
				skip[k] = true
			}
		}
		base := Event{Title: unescape(ev["SUMMARY"].Value), Location: unescape(ev["LOCATION"].Value), AllDay: allDay}
		length := eventLength(ev, start, allDay)
		for _, at := range expand(start, ev["RRULE"].Value, to) {
			if !at.Before(to) || skip[at.Unix()] {
				continue
			}

			// Started earlier: listed only while still running.
			if at.Before(from) && !at.Add(length).After(from) {
				continue
			}
			e := base
			e.Start = at
			if length > 0 {
				e.End = at.Add(length)
			}
			out = append(out, e)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	if len(out) > icalMaxEvents {
		out = out[:icalMaxEvents]
	}
	return out
}

// eventLength is how long an event runs: DTEND, else DURATION, else one
// day for an all-day event and nothing for a timed one (RFC 5545 3.6.1).
func eventLength(ev vevent, start time.Time, allDay bool) time.Duration {
	if end, _, ok := icalTime(ev["DTEND"]); ok && end.After(start) {
		return end.Sub(start)
	}
	if d, ok := icalDuration(ev["DURATION"].Value); ok {
		return d
	}
	if allDay {
		return hoursPerDay * time.Hour
	}

	return 0
}

// icalDurationRe matches "P1W", "P2D", "PT1H30M", "P1DT2H".
var icalDurationRe = regexp.MustCompile(`^\+?P(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)

// icalDuration parses a positive DURATION value.
func icalDuration(v string) (time.Duration, bool) {
	m := icalDurationRe.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return 0, false
	}
	units := []time.Duration{daysPerWeek * hoursPerDay * time.Hour, hoursPerDay * time.Hour, time.Hour, time.Minute, time.Second}
	var d time.Duration
	for i, unit := range units {
		n, _ := strconv.Atoi(m[i+1])
		d += time.Duration(n) * unit
	}

	return d, d > 0
}

func parseEvents(text string) []vevent {
	var events []vevent
	var cur vevent
	for _, line := range unfold(text) {
		switch {
		case line == "BEGIN:VEVENT":
			cur = vevent{}
			continue
		case line == "END:VEVENT":
			if cur != nil {
				events = append(events, cur)
			}
			cur = nil
			continue
		}
		if cur == nil {
			continue
		}
		head, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		parts := strings.Split(head, ";")
		prop := icalProp{Value: value, Params: map[string]string{}}
		for _, p := range parts[1:] {
			if k, v, ok := strings.Cut(p, "="); ok {
				prop.Params[strings.ToUpper(k)] = strings.Trim(v, `"`)
			}
		}
		name := strings.ToUpper(parts[0])

		// EXDATE may repeat; keep every value.
		if old, ok := cur[name]; ok && name == "EXDATE" {
			prop.Value = old.Value + "," + prop.Value
		}
		cur[name] = prop
	}
	return events
}

// unfold joins continuation lines (RFC 5545 3.1).
func unfold(text string) []string {
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if (strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += line[1:]
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

func unescape(s string) string {
	return strings.NewReplacer(`\n`, " ", `\N`, " ", `\,`, ",", `\;`, ";", `\\`, `\`).Replace(s)
}

// zones caches zone lookups by TZID, failed ones as the fallback.
var zones sync.Map

// zone resolves a TZID: an IANA name, a Windows name (Outlook) or an
// IANA name behind a prefix (old Thunderbird):
//
//	Europe/Berlin · W. Europe Standard Time · /mozilla.org/20050126_1/Europe/Berlin
//
// Unknown ones fall back to icalZone.
func zone(name string) *time.Location {
	if loc, ok := zones.Load(name); ok {
		return loc.(*time.Location)
	}
	loc := lookupZone(name)
	zones.Store(name, loc)

	return loc
}

func lookupZone(name string) *time.Location {
	if iana, ok := windowsZones[name]; ok {
		name = iana
	}
	for rest := name; rest != ""; {
		if loc, err := time.LoadLocation(rest); err == nil {
			return loc
		}
		_, after, found := strings.Cut(rest, "/")
		if !found {
			break
		}
		rest = after
	}

	loc, _ := time.LoadLocation(icalZone)
	if loc == nil {
		return time.UTC
	}
	return loc
}

// icalTime parses DATE, UTC and zoned/floating DATE-TIME values.
func icalTime(p icalProp) (time.Time, bool, bool) {
	v := strings.TrimSpace(p.Value)
	if len(v) == len(icalDate) {
		t, err := time.ParseInLocation(icalDate, v, zone(icalZone))
		return t, true, err == nil
	}
	if strings.HasSuffix(v, "Z") {
		t, err := time.Parse(icalDateTime, strings.TrimSuffix(v, "Z"))
		return t, false, err == nil
	}
	t, err := time.ParseInLocation(icalDateTime, v, zone(firstNonBlank(p.Params["TZID"], icalZone)))
	return t, false, err == nil
}

func firstNonBlank(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func exdates(ev vevent) map[int64]bool {
	out := map[int64]bool{}
	p, ok := ev["EXDATE"]
	if !ok {
		return out
	}
	for _, v := range strings.Split(p.Value, ",") {
		if t, _, ok := icalTime(icalProp{Value: v, Params: p.Params}); ok {
			out[t.Unix()] = true
		}
	}
	return out
}

var weekdays = map[string]time.Weekday{
	"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday,
	"TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday,
}

// expand lists the occurrences of a rule up to (excluding) end.
func expand(start time.Time, rule string, end time.Time) []time.Time {
	if rule == "" {
		return []time.Time{start}
	}
	parts := map[string]string{}
	for _, p := range strings.Split(rule, ";") {
		if k, v, ok := strings.Cut(p, "="); ok {
			parts[strings.ToUpper(k)] = v
		}
	}
	interval, _ := strconv.Atoi(parts["INTERVAL"])
	interval = max(interval, 1)
	count, _ := strconv.Atoi(parts["COUNT"])
	if until, _, ok := icalTime(icalProp{Value: parts["UNTIL"], Params: map[string]string{}}); ok && until.Before(end) {
		end = until.Add(time.Second)
	}

	days := byDays(parts["BYDAY"])
	var plain []time.Weekday
	for _, d := range days {
		plain = append(plain, d.day)
	}

	var out []time.Time
	emit := func(t time.Time) bool {
		if !t.Before(end) || (count > 0 && len(out) >= count) {
			return false
		}
		out = append(out, t)
		return true
	}

	for step := 0; step < icalMaxSteps; step++ {
		switch parts["FREQ"] {
		case "DAILY":
			if !emit(start.AddDate(0, 0, step*interval)) {
				return out
			}
		case "WEEKLY":
			if !weekly(start, step*interval, plain, emit) {
				return out
			}
		case "MONTHLY":
			if !inMonth(start, monthStart(start, 0, step*interval), parts, days, emit) {
				return out
			}
		case "YEARLY":
			for _, m := range byMonths(parts["BYMONTH"], start.Month()) {
				if !inMonth(start, monthStart(start, step*interval, int(m-start.Month())), parts, days, emit) {
					return out
				}
			}
		default:
			return []time.Time{start}
		}
	}
	return out
}

// byDay is one BYDAY entry: "-1FR" → {-1, Friday}; n 0 means every.
type byDay struct {
	n   int
	day time.Weekday
}

// weekdayCode is the length of "MO".
const weekdayCode = 2

func byDays(list string) []byDay {
	var out []byDay
	for _, d := range strings.Split(list, ",") {
		d = strings.ToUpper(strings.TrimSpace(d))
		if len(d) < weekdayCode {
			continue
		}
		wd, ok := weekdays[d[len(d)-weekdayCode:]]
		if !ok {
			continue
		}
		n, err := strconv.Atoi(d[:len(d)-weekdayCode])
		if err != nil && len(d) > weekdayCode {
			continue
		}
		out = append(out, byDay{n, wd})
	}
	return out
}

// byMonths lists BYMONTH (1–12) sorted, else the start month.
func byMonths(list string, fallback time.Month) []time.Month {
	var out []time.Month
	for _, s := range strings.Split(list, ",") {
		if m, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && m >= 1 && m <= 12 {
			out = append(out, time.Month(m))
		}
	}
	if len(out) == 0 {
		return []time.Month{fallback}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// monthStart is the first of the month years/months after start's, at
// start's time of day.
func monthStart(start time.Time, years, months int) time.Time {
	return time.Date(start.Year()+years, start.Month()+time.Month(months), 1,
		start.Hour(), start.Minute(), start.Second(), 0, start.Location())
}

// inMonth emits the rule's days of the month beginning at first, from
// start on; false once emit refuses.
func inMonth(start, first time.Time, parts map[string]string, days []byDay, emit func(time.Time) bool) bool {
	for _, d := range monthDays(first, parts["BYMONTHDAY"], days, start.Day()) {
		t := first.AddDate(0, 0, d-1)
		if t.Before(start) {
			continue
		}
		if !emit(t) {
			return false
		}
	}
	return true
}

// monthDays lists the days (1–31) a rule hits in the month of first:
// BYMONTHDAY (−1 = last), filtered by BYDAY's weekdays if both are set;
// else BYDAY (1MO = first Monday, -1FR = last Friday, MO = every Monday);
// else start's day, if the month has it.
func monthDays(first time.Time, monthDays string, days []byDay, startDay int) []int {
	last := first.AddDate(0, 1, -1).Day()
	weekdayOf := func(d int) time.Weekday { return first.AddDate(0, 0, d-1).Weekday() }

	var out []int
	for _, s := range strings.Split(monthDays, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(s))
		if err != nil {
			continue
		}
		if n < 0 {
			n += last + 1
		}
		if n < 1 || n > last {
			continue
		}
		if len(days) > 0 && !slices.ContainsFunc(days, func(b byDay) bool { return b.day == weekdayOf(n) }) {
			continue
		}
		out = append(out, n)
	}
	if monthDays != "" {
		return sortedDays(out)
	}

	for _, b := range days {
		firstHit := 1 + (int(b.day)-int(first.Weekday())+daysPerWeek)%daysPerWeek
		lastHit := last - (int(weekdayOf(last))-int(b.day)+daysPerWeek)%daysPerWeek
		switch {
		case b.n == 0:
			for d := firstHit; d <= last; d += daysPerWeek {
				out = append(out, d)
			}
		case b.n > 0:
			out = append(out, firstHit+(b.n-1)*daysPerWeek)
		default:
			out = append(out, lastHit+(b.n+1)*daysPerWeek)
		}
	}
	if len(days) == 0 {
		out = append(out, startDay)
	}

	// Drop days the month lacks: a 5th Monday, a 31st.
	return sortedDays(slices.DeleteFunc(out, func(d int) bool { return d < 1 || d > last }))
}

func sortedDays(days []int) []int {
	slices.Sort(days)
	return slices.Compact(days)
}

// weekly emits the BYDAY days of the week week weeks after start (only
// start's own weekday without BYDAY); false once emit refuses.
func weekly(start time.Time, week int, days []time.Weekday, emit func(time.Time) bool) bool {
	if len(days) == 0 {
		return emit(start.AddDate(0, 0, week*daysPerWeek))
	}
	monday := start.AddDate(0, 0, -((int(start.Weekday())+6)%daysPerWeek)+week*daysPerWeek)
	sort.Slice(days, func(i, j int) bool { return (days[i]+6)%daysPerWeek < (days[j]+6)%daysPerWeek })
	for _, wd := range days {
		t := monday.AddDate(0, 0, (int(wd)+6)%daysPerWeek)
		if t.Before(start) {
			continue
		}
		if !emit(t) {
			return false
		}
	}
	return true
}

func init() {
	Register(CalendarSource)
}
