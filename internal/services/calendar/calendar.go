// Package calendar lists upcoming deadlines (tax dates from space settings,
// hints with a due date) and renders them as a per-user iCal feed.
//
//	GET /calendar.ics?token=…  (read-scoped API token)
package calendar

import (
	"database/sql"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/metrics"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/hints"
)

const (
	feedHorizonDays = 400
	prodID          = "-//shrippen//andon//DE"
	dayLayout       = "20060102"
	stampLayout     = "20060102T150405Z"
	taxRulePrefix   = "tax."
	foldOctets      = 75 // RFC 5545 3.1: content lines fold after 75 octets
	hintPath        = "/hints#hint-"
	taxPath         = "/settings/finance#tax"
)

// Deadline is one dated entry, already translated.
type Deadline struct {
	UID  string
	Due  time.Time
	Text string
	Desc string // what it is about, e.g. the hint's reason or the space
	Path string // its page in Andon, e.g. "/hints#hint-42"
}

// TaxDeadlines returns the tax dates of every space who can reach, within days.
func TaxDeadlines(d *sql.DB, who *access.Principal, today time.Time, days int) ([]Deadline, error) {
	ids := make([]int64, 0, len(who.Spaces))
	for id := range who.Spaces {
		ids = append(ids, id)
	}
	spaces, err := content.Spaces(d, ids)
	if err != nil {
		return nil, err
	}

	var out []Deadline
	for _, sp := range spaces {
		tax, ok := metrics.ParseTaxSettings(sp.Settings)
		if !ok {
			continue
		}
		for _, item := range metrics.UpcomingDeadlines(tax, today, days) {
			text := i18n.T("deadline."+item.Kind, who.Locale, map[string]any{"period": item.Period, "year": item.Year})
			uid := fmt.Sprintf("%d-%s-%s", sp.ID, item.Kind, item.Due.Format(time.DateOnly))
			path := "/spaces/" + strconv.FormatInt(sp.ID, 10) + taxPath
			out = append(out, Deadline{UID: uid, Due: item.Due, Text: text, Desc: sp.Name, Path: path})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Due.Before(out[j].Due) })
	return out, nil
}

// Feed renders who's deadlines and due hints as an iCalendar document.
func Feed(d *sql.DB, who *access.Principal, now time.Time, baseURL string) (string, error) {
	items, err := TaxDeadlines(d, who, now, feedHorizonDays)
	if err != nil {
		return "", err
	}

	open, err := hints.Active(d, who, enums.SeverityInfo, nil, 0)
	if err != nil {
		return "", err
	}
	for _, h := range open {
		if h.Due == "" || strings.HasPrefix(h.Rule, taxRulePrefix) {
			continue
		}
		due, err := time.Parse(time.DateOnly, h.Due[:min(len(h.Due), len(time.DateOnly))])
		if err != nil {
			continue
		}
		items = append(items, Deadline{UID: fmt.Sprintf("hint-%d", h.ID), Due: due, Text: h.Title, Desc: h.Why,
			Path: hintPath + strconv.FormatInt(h.ID, 10)})
	}
	items = merge(items)

	stamp := now.UTC().Format(stampLayout)
	lines := []string{
		"BEGIN:VCALENDAR", "VERSION:2.0", "PRODID:" + prodID, "CALSCALE:GREGORIAN",
		"X-WR-CALNAME:" + escape(i18n.T("calendar.name", who.Locale, nil)),
	}
	base := strings.TrimRight(baseURL, "/")
	for _, item := range items {
		lines = append(lines, event(item, stamp, base)...)
	}
	lines = append(lines, "END:VCALENDAR")
	for i, line := range lines {
		lines[i] = fold(line)
	}
	return strings.Join(lines, "\r\n") + "\r\n", nil
}

// merge joins entries with the same day and text into the first one; the
// calendar would show them twice. Their distinct descriptions become
// lines: two hosts offline on one day = one event naming both.
func merge(items []Deadline) []Deadline {
	type key struct {
		due  string
		text string
	}
	at := map[key]int{}
	var out []Deadline
	for _, item := range items {
		k := key{item.Due.Format(time.DateOnly), item.Text}
		i, seen := at[k]
		if !seen {
			at[k] = len(out)
			out = append(out, item)
			continue
		}

		// Same event: keep each reason once.
		first := &out[i]
		if item.Desc == "" || slices.Contains(strings.Split(first.Desc, "\n"), item.Desc) {
			continue
		}
		first.Desc = strings.TrimPrefix(first.Desc+"\n"+item.Desc, "\n")
	}
	return out
}

// event is one all-day VEVENT (DTEND is exclusive: the next day); base
// makes its page absolute ("https://andon.example").
func event(item Deadline, stamp, base string) []string {
	lines := []string{
		"BEGIN:VEVENT",
		"UID:" + item.UID + "@andon",
		"DTSTAMP:" + stamp,
		"DTSTART;VALUE=DATE:" + item.Due.Format(dayLayout),
		"DTEND;VALUE=DATE:" + item.Due.AddDate(0, 0, 1).Format(dayLayout),
		"SUMMARY:" + escape(item.Text),
	}
	if item.Desc != "" {
		lines = append(lines, "DESCRIPTION:"+escape(item.Desc))
	}
	if item.Path != "" && base != "" {
		lines = append(lines, "URL:"+base+item.Path)
	}
	return append(lines, "END:VEVENT")
}

// fold splits a content line after 75 octets, never inside a UTF-8
// character; each continuation starts with a space (RFC 5545 3.1).
func fold(line string) string {
	var b strings.Builder
	limit := foldOctets
	for len(line) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(line[cut]) {
			cut--
		}
		b.WriteString(line[:cut])
		b.WriteString("\r\n ")
		line = line[cut:]
		limit = foldOctets - 1 // the leading space counts
	}
	b.WriteString(line)
	return b.String()
}

// escape applies RFC 5545 TEXT escaping: "a, b; c" -> "a\, b\; c".
func escape(text string) string {
	return strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\n", `\n`).Replace(text)
}
