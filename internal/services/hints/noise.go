package hints

// Noise: how many hints arrive per day, and which rules keep coming and
// going on their own (resolved, then reopened by a later run):
//
//	new per day  ▁▂▅▃▂▁▁▂   freshrss.stale_feed  back 11× in 14 days

import (
	"database/sql"
	"sort"
	"time"

	"andon/internal/enums"
	"andon/internal/repos/data"
	"andon/internal/services/access"
)

// NoiseReport is the hint traffic of the caller's spaces.
type NoiseReport struct {
	Daily    []int // new or reopened hints per day, oldest first
	Flapping []Flap
	// Open counts the hints open at the end of each day (today: now) per
	// level, oldest first.
	Open map[enums.Severity][]int
}

// Flap is a rule whose hints came back on their own.
type Flap struct {
	Rule    string
	Returns int
}

const flapShown = 5

// Noise counts the last days' hint traffic visible to who.
func Noise(d *sql.DB, who *access.Principal, now time.Time, days int) (NoiseReport, error) {
	ids := make([]int64, 0, len(who.Spaces))
	for id := range who.Spaces {
		ids = append(ids, id)
	}
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	since := today.AddDate(0, 0, -days+1)
	kinds := []string{string(enums.EventOpened), string(enums.EventReopened)}
	events, err := data.HintEventsSince(d, ids, kinds, since, noisyLimit)
	if err != nil {
		return NoiseReport{}, err
	}

	report := NoiseReport{Daily: make([]int, days)}
	if report.Open, err = openPerDay(d, who, ids, since, now, days); err != nil {
		return NoiseReport{}, err
	}
	returns := map[string]int{}
	for _, e := range events {
		if e.Owner != 0 && e.Owner != who.UserID {
			continue
		}
		at := e.At.UTC()
		i := int(time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, time.UTC).Sub(since).Hours() / 24)
		if i >= 0 && i < days {
			report.Daily[i]++
		}
		if e.Kind == string(enums.EventReopened) {
			returns[e.Rule]++
		}
	}
	for rule, n := range returns {
		report.Flapping = append(report.Flapping, Flap{Rule: rule, Returns: n})
	}
	sort.Slice(report.Flapping, func(i, j int) bool { return report.Flapping[i].Returns > report.Flapping[j].Returns })
	if len(report.Flapping) > flapShown {
		report.Flapping = report.Flapping[:flapShown]
	}
	return report, nil
}

// levels are the severity bands the open counts are kept in.
var levels = []enums.Severity{enums.SeverityInfo, enums.SeverityWarn, enums.SeverityCritical}

// openPerDay counts, per level and day, the hints open at the day's end:
//
//	appeared day 1, resolved day 3  →  counted on days 1 and 2
func openPerDay(d *sql.DB, who *access.Principal, spaceIDs []int64, since, now time.Time, days int) (map[enums.Severity][]int, error) {
	found, err := data.HintsTouching(d, spaceIDs, who.UserID, since)
	if err != nil {
		return nil, err
	}
	out := map[enums.Severity][]int{}
	for _, l := range levels {
		out[l] = make([]int, days)
	}
	for _, h := range found {
		band := levelBand(h.Severity)
		for i := range days {
			end := since.AddDate(0, 0, i+1)
			if i == days-1 {
				end = now
			}
			if !h.FirstSeen.After(end) && (h.ResolvedAt == nil || h.ResolvedAt.After(end)) {
				out[band][i]++
			}
		}
	}
	return out, nil
}

// levelBand maps a severity to its band (info, warn, critical).
func levelBand(s enums.Severity) enums.Severity {
	for i := len(levels) - 1; i >= 0; i-- {
		if s >= levels[i] {
			return levels[i]
		}
	}
	return levels[0]
}
