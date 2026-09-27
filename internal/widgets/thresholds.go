package widgets

// Colour thresholds typed into a tile, one per line:
//
//	queue > 10 gelb
//	Temp >= 26 rot
//
// The name matches a field's label (any case); the level word is
// gelb/yellow/warn or rot/red/fail (default: warn).

import (
	"regexp"
	"strconv"
	"strings"
)

// Threshold is one "name op limit level" line.
type Threshold struct {
	Name  string // lower case
	Op    string
	Limit float64
	Level string // "warn" or "fail"
}

var thresholdLine = regexp.MustCompile(`^(.+?)\s*(>=|<=|>|<)\s*(-?[0-9]+(?:[.,][0-9]+)?)\s*(\S*)$`)

// Threshold levels as the templates' pill states.
const (
	levelWarn = "warn"
	levelFail = "fail"
)

func parseThresholds(text string) []Threshold {
	var out []Threshold
	for _, line := range strings.Split(text, "\n") {
		m := thresholdLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		limit, err := strconv.ParseFloat(strings.ReplaceAll(m[3], ",", "."), 64)
		if err != nil {
			continue
		}
		level := levelWarn
		switch strings.ToLower(m[4]) {
		case "rot", "red", "fail", "kritisch", "critical":
			level = levelFail
		}
		out = append(out, Threshold{Name: strings.ToLower(strings.TrimSpace(m[1])), Op: m[2], Limit: limit, Level: level})
	}
	return out
}

// levelOf is the worst level any threshold for name gives v, "" if none.
func levelOf(name string, v float64, list []Threshold) string {
	name = strings.ToLower(strings.TrimSpace(name))
	level := ""
	for _, t := range list {
		if t.Name != name {
			continue
		}
		hit := false
		switch t.Op {
		case ">":
			hit = v > t.Limit
		case ">=":
			hit = v >= t.Limit
		case "<":
			hit = v < t.Limit
		case "<=":
			hit = v <= t.Limit
		}
		if hit && (level == "" || t.Level == levelFail) {
			level = t.Level
		}
	}
	return level
}

// parsePairs reads "name = value" lines into a map keyed by lower-case
// name (labels and units per field or entity).
func parsePairs(text string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(text, "\n") {
		name, value, ok := strings.Cut(line, fieldSep)
		if name = strings.ToLower(strings.TrimSpace(name)); ok && name != "" {
			out[name] = strings.TrimSpace(value)
		}
	}
	return out
}

// numberOf reads a JSON-ish value as a number.
func numberOf(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(n), ",", "."), 64)
		return f, err == nil
	}
	return 0, false
}
