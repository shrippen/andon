// Package rules is the rule contract: pure functions from datasets to
// findings.
//
//	Register("kimai.timer_running_long", ServiceKimai, map[string]any{"hours": 10}, func(...) ...)
//
// cfg  = defaults overridden by the space settings (settings["rules"][rule id])
// env  = today, space settings, other datasets of the same space (cross rules)
// Rules never do I/O; the analysis service feeds them.
package rules

import (
	"math"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"andon/internal/enums"
)

// Scope groups rules that fire together: one per ServiceType, plus these
// two pseudo-scopes.
const (
	Cross     = "cross"
	Deadlines = "deadlines"
	Enabled   = "enabled"
	// Escalate is every rule's "raise to critical after N open days"
	// setting; 0 keeps the rule's own severity.
	Escalate = "escalate_days"
	// MaxAge drops findings older than N days (their "days" param);
	// Exclude drops findings naming a listed name or pattern (see Filter).
	MaxAge  = "max_age_days"
	Exclude = "exclude"
)

// Finding is one problem found by a rule, e.g. an overdue invoice.
type Finding struct {
	Fingerprint string
	Rule        string
	Severity    enums.Severity
	Message     string
	Params      map[string]any
	ActionURL   string
	ActionLabel string
	Due         string
	Sources     []string
	// EscalateDays > 0 raises the hint to critical once it has been open
	// that long (rule setting escalate_days, filled in by the analysis).
	EscalateDays int
}

// Env is what a rule sees beyond its own service's dataset: today, the
// space's settings, and (for cross-service rules) every dataset of the
// same space.
type Env struct {
	Today    time.Time
	Settings map[string]any
	Datasets map[string]any // keyed by ServiceType value, e.g. "kimai" -> *sources.KimaiDataset
	Options  map[string]map[string]any
}

// RuleFunc runs one rule against a dataset and its config.
type RuleFunc func(data any, cfg map[string]any, env Env) []Finding

// Spec is a registered rule: its id, scope, default config and function.
type Spec struct {
	ID       string
	Scope    string
	Defaults map[string]any
	Run      RuleFunc
}

var registry = map[string]Spec{}

// Register adds a rule to the process-wide registry. scope is a
// ServiceType value, Cross, or Deadlines.
func Register(id string, scope string, defaults map[string]any, fn RuleFunc) {
	if _, taken := registry[id]; taken {
		panic("rules: duplicate rule id " + id)
	}
	merged := map[string]any{Enabled: true, Escalate: 0.0, MaxAge: 0.0, Exclude: ""}
	for k, v := range defaults {
		merged[k] = v
	}
	registry[id] = Spec{ID: id, Scope: scope, Defaults: merged, Run: fn}
}

// needs lists, per rule, the services without which a run of it says
// nothing: when one of them failed, the rule's hints stay as they were.
var needs = map[string][]string{}

// Needs names services a rule's result depends on (see Incomplete).
func Needs(id string, services ...string) {
	needs[id] = services
}

// Incomplete reports whether a service the rule needs failed this run.
func (s Spec) Incomplete(env Env) bool {
	failed, _ := env.Datasets[FailedDataset].([]Failed)
	for _, f := range failed {
		if slices.Contains(needs[s.ID], f.Service) {
			return true
		}
	}
	return false
}

// ForScope returns every rule registered for one scope.
func ForScope(scope string) []Spec {
	var out []Spec
	for _, spec := range registry {
		if spec.Scope == scope {
			out = append(out, spec)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// AllRules returns every registered rule, sorted by id.
func AllRules() []Spec {
	out := make([]Spec, 0, len(registry))
	for _, spec := range registry {
		out = append(out, spec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Config merges a rule's defaults with the space's own override
// (settings["rules"][spec.ID]), keeping only known keys.
func Config(spec Spec, settings map[string]any) map[string]any {
	out := make(map[string]any, len(spec.Defaults))
	for k, v := range spec.Defaults {
		out[k] = v
	}
	rulesSettings, _ := settings["rules"].(map[string]any)
	custom, _ := rulesSettings[spec.ID].(map[string]any)
	for k, v := range custom {
		if _, known := spec.Defaults[k]; known {
			out[k] = v
		}
	}
	return out
}

// Money is a typed finding parameter, formatted per reader locale.
func Money(value float64, currency string) map[string]any {
	if currency == "" {
		currency = "EUR"
	}
	return map[string]any{"$money": round2(value), "currency": currency}
}

// Day is a typed finding parameter for a date.
func Day(value time.Time) map[string]any {
	return map[string]any{"$day": value.Format("2006-01-02")}
}

// DayStr is Day for an already-formatted "YYYY-MM-DD" (or longer ISO)
// string, matching call sites that only have the raw string.
func DayStr(value string) map[string]any {
	if len(value) > 10 {
		value = value[:10]
	}
	return map[string]any{"$day": value}
}

// Num is a typed finding parameter for a plain number.
func Num(value float64, digits int) map[string]any {
	return map[string]any{"$num": value, "digits": digits}
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}

// ── cfg helpers: cfg values arrive as map[string]any (from JSON/YAML), so
// these do the float64/int/bool/string coercions rule bodies need. ──

// Filter drops a rule's findings the space excluded: older than MaxAge
// days, or naming an Exclude entry. Entries are comma-separated, case
// does not matter; "*" is a wildcard, a plain word matches as part
// ("Heizung*, sonos").
func Filter(found []Finding, cfg map[string]any) []Finding {
	maxAge := cfgFloat(cfg, MaxAge)
	var patterns []string
	for _, p := range strings.Split(strings.ToLower(cfgString(cfg, Exclude)), ",") {
		if p = strings.TrimSpace(p); p != "" {
			patterns = append(patterns, p)
		}
	}
	if maxAge <= 0 && len(patterns) == 0 {
		return found
	}

	var out []Finding
	for _, f := range found {
		if age, ok := numberOf(f.Params["days"]); ok && maxAge > 0 && age > maxAge {
			continue
		}
		if excluded(f.Params, patterns) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// excluded reports whether a text param matches one of the patterns.
func excluded(params map[string]any, patterns []string) bool {
	for _, v := range params {
		text, ok := v.(string)
		if !ok {
			continue
		}
		text = strings.ToLower(text)
		for _, p := range patterns {
			if matched, _ := path.Match(p, text); matched || (!strings.Contains(p, "*") && strings.Contains(text, p)) {
				return true
			}
		}
	}
	return false
}

// numberOf reads an int or float param.
func numberOf(v any) (float64, bool) {
	switch n := v.(type) {
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case float64:
		return n, true
	}
	return 0, false
}

// EscalateDays reads a rule config's escalation setting (0 = off).
func EscalateDays(cfg map[string]any) int {
	return int(cfgFloat(cfg, Escalate))
}

func cfgFloat(cfg map[string]any, key string) float64 {
	switch v := cfg[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	default:
		return 0
	}
}

func cfgInt(cfg map[string]any, key string) int {
	return int(cfgFloat(cfg, key))
}

func cfgString(cfg map[string]any, key string) string {
	v, _ := cfg[key].(string)
	return v
}

func cfgBool(cfg map[string]any, key string) bool {
	b, _ := cfg[key].(bool)
	return b
}
