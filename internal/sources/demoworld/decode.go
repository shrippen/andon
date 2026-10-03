package demoworld

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Decode fills v from the world's section at path ("it.monitoring"), the
// way the demo datasets read it:
//
//	{"de": …, "en": …}       → the German text
//	"{{vendors.nordhost.name}}" → that value (a list entry by its id);
//	                            inside a longer text, its text
//	"@-3h", "@+2d", "@-1d6h" → a time relative to now (RFC 3339; the
//	                            sign holds for every part)
//	"@date-2"                → a day relative to today in UTC, like the
//	                            rules' Today (2006-01-02)
//	"@date-2 14:00"          → that day at that local time (RFC 3339)
//	"cert_days"              → matches CertDays (underscores dropped)
func Decode(path string, now time.Time, v any) error {
	node, err := lookup(tree(), strings.Split(path, "."))
	if err != nil {
		return err
	}
	b, err := json.Marshal(resolve(node, now))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// MustDecode is Decode for sections the demo cannot do without.
func MustDecode(path string, now time.Time, v any) {
	if err := Decode(path, now, v); err != nil {
		panic("demoworld: " + err.Error())
	}
}

var (
	generic any
	refPat  = regexp.MustCompile(`\{\{([a-z_]+(?:\.[A-Za-z0-9_-]+)+)\}\}`)
	relPat  = regexp.MustCompile(`^@[+-](\d+[dhm])+$`)
	partPat = regexp.MustCompile(`(\d+)([dhm])`)
	datePat = regexp.MustCompile(`^@date([+-]\d+)(?: (\d\d):(\d\d))?$`)
)

// tree is the world as plain JSON values, read once.
func tree() any {
	if generic == nil {
		if err := json.Unmarshal(raw, &generic); err != nil {
			panic("demoworld: " + err.Error())
		}
	}
	return generic
}

// lookup walks keys; in a list a key names the entry with that id, or
// else its index ("inventory.disks.0").
func lookup(node any, keys []string) (any, error) {
	for _, k := range keys {
		switch n := node.(type) {
		case map[string]any:
			next, ok := n[k]
			if !ok {
				return nil, fmt.Errorf("no %q in the world", strings.Join(keys, "."))
			}
			node = next
		case []any:
			found := false
			for _, item := range n {
				if m, ok := item.(map[string]any); ok && fmt.Sprint(m["id"]) == k {
					node, found = m, true
					break
				}
			}
			if i, err := strconv.Atoi(k); !found && err == nil && i >= 0 && i < len(n) {
				node, found = n[i], true
			}
			if !found {
				return nil, fmt.Errorf("no id %q in %q", k, strings.Join(keys, "."))
			}
		default:
			return nil, fmt.Errorf("%q is no object", strings.Join(keys, "."))
		}
	}
	return node, nil
}

// resolve applies the rules of Decode to a value and everything in it.
func resolve(node any, now time.Time) any {
	switch n := node.(type) {
	case map[string]any:
		if de, ok := n["de"]; ok && len(n) <= 2 {
			if _, en := n["en"]; en || len(n) == 1 {
				return resolve(de, now)
			}
		}
		out := make(map[string]any, len(n))
		for k, v := range n {
			out[strings.ReplaceAll(k, "_", "")] = resolve(v, now)
		}
		return out
	case []any:
		out := make([]any, len(n))
		for i, v := range n {
			out[i] = resolve(v, now)
		}
		return out
	case string:
		return resolveText(n, now)
	}
	return node
}

func resolveText(s string, now time.Time) any {
	if m := datePat.FindStringSubmatch(s); m != nil {
		days, _ := strconv.Atoi(m[1])
		if m[2] == "" {
			return now.UTC().AddDate(0, 0, days).Format(time.DateOnly)
		}
		h, _ := strconv.Atoi(m[2])
		mins, _ := strconv.Atoi(m[3])
		day := time.Date(now.Year(), now.Month(), now.Day(), h, mins, 0, 0, now.Location())
		return day.AddDate(0, 0, days).Format(time.RFC3339)
	}
	if relPat.MatchString(s) {
		at, sign := now.UTC(), 1
		if s[1] == '-' {
			sign = -1
		}
		for _, p := range partPat.FindAllStringSubmatch(s, -1) {
			n, _ := strconv.Atoi(p[1])
			n *= sign
			switch p[2] {
			case "d":
				at = at.AddDate(0, 0, n)
			case "h":
				at = at.Add(time.Duration(n) * time.Hour)
			case "m":
				at = at.Add(time.Duration(n) * time.Minute)
			}
		}
		return at.Format(time.RFC3339)
	}

	// A whole-string reference keeps its type (a number stays a number).
	if m := refPat.FindStringSubmatch(s); m != nil && m[0] == s {
		return resolve(ref(m[1]), now)
	}
	return refPat.ReplaceAllStringFunc(s, func(r string) string {
		return fmt.Sprint(resolve(ref(refPat.FindStringSubmatch(r)[1]), now))
	})
}

func ref(path string) any {
	v, err := lookup(tree(), strings.Split(path, "."))
	if err != nil {
		panic("demoworld: " + err.Error())
	}
	return v
}
