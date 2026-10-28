package spaces

// Custom rule rows of the settings form:
//
//	cr_count=2, cr.0.title=…, cr.0.service=sabnzbd, cr.0.path=Slots, cr.0.op=">", cr.0.value=10,
//	cr.0.severity=20, cr.0.delete=on → settings.custom_rules (row 1 is the empty "new" row)

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/rules"
)

const (
	rowPrefix = "cr."
	maxRules  = 50
	idBase    = 36
)

// CustomRows returns the rules plus one empty row for a new one.
func CustomRows(settings map[string]any) []rules.CustomRule {
	return append(rules.CustomRules(settings), rules.CustomRule{Op: ">", Severity: enums.SeverityWarn})
}

// ErrThreshold: a custom rule's threshold is no number in the user's
// notation; saving 0 instead would fire the rule on every value.
var ErrThreshold = errors.New("custom rule threshold")

// ParseCustomRules turns the form rows into settings.custom_rules; the
// threshold reads in notation n.
func ParseCustomRules(get func(string) string, n enums.Notation) ([]any, error) {
	count, _ := strconv.Atoi(get("cr_count"))
	count = min(count, maxRules+1)
	out := []any{}
	for i := 0; i < count; i++ {
		field := func(name string) string { return strings.TrimSpace(get(rowPrefix + strconv.Itoa(i) + "." + name)) }
		if field("delete") != "" || field("path") == "" || field("service") == "" {
			continue
		}
		op := field("op")
		if !validOp(op) {
			op = ">"
		}
		// An empty threshold is 0 ("> 0").
		var value float64
		if raw := field("value"); raw != "" {
			v, err := i18n.ParseNum(raw, n)
			if err != nil {
				return nil, ErrThreshold
			}
			value = v
		}
		severity, _ := strconv.Atoi(field("severity"))
		id := field("id")
		if id == "" {
			id = strconv.FormatInt(time.Now().UnixNano(), idBase) + strconv.Itoa(i)
		}
		title := field("title")
		if title == "" {
			title = field("service") + " " + field("path")
		}
		out = append(out, map[string]any{"id": id, "title": title, "service": field("service"), "path": field("path"),
			"op": op, "value": value, "severity": float64(clampSeverity(severity))})
	}
	return out, nil
}

func validOp(op string) bool {
	for _, o := range rules.CustomOps {
		if o == op {
			return true
		}
	}
	return false
}

func clampSeverity(s int) enums.Severity {
	switch enums.Severity(s) {
	case enums.SeverityInfo, enums.SeverityWarn, enums.SeverityCritical:
		return enums.Severity(s)
	}
	return enums.SeverityWarn
}
