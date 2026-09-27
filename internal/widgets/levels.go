package widgets

import "andon/internal/enums"

// LevelCount is one level's share of the open hints, for the hints
// tile's stacked bar.
type LevelCount struct {
	Severity enums.Severity
	N        int
	W        float64 // percent of the bar
}

// barLevels are the drawn levels, most urgent first.
var barLevels = []enums.Severity{enums.SeverityCritical, enums.SeverityWarn, enums.SeverityInfo}

// LevelBar counts hints per level; nil when there are none.
func LevelBar(sevs []enums.Severity) []LevelCount {
	if len(sevs) == 0 {
		return nil
	}
	var out []LevelCount
	for _, level := range barLevels {
		n := 0
		for _, s := range sevs {
			if s.Key() == level.Key() {
				n++
			}
		}
		if n > 0 {
			out = append(out, LevelCount{Severity: level, N: n, W: float64(n) / float64(len(sevs)) * pctFull})
		}
	}
	return out
}
