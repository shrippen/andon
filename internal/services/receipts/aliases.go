package receipts

import "slices"

// Aliases are groups of names for the same vendor, learned from confirmed
// links: an expense from "Hetzner" linked to a scan from "Hetzner Online
// GmbH" makes both names count as one from then on.
type Aliases [][]string

// equivalents lists the names counted as name (normalized), name itself
// included.
func (a Aliases) equivalents(name string) []string {
	key := normalize(name)
	if key == "" {
		return nil
	}
	for _, group := range a {
		if slices.Contains(group, key) {
			return group
		}
	}
	return []string{key}
}

// learn puts two names into one group, merging groups they already
// belong to. It reports whether anything changed.
func (a *Aliases) learn(left, right string) bool {
	l, r := normalize(left), normalize(right)
	if l == "" || r == "" || l == r {
		return false
	}
	merged := []string{l, r}
	var keep Aliases
	for _, group := range *a {
		if slices.Contains(group, l) || slices.Contains(group, r) {
			merged = append(merged, group...)
			continue
		}
		keep = append(keep, group)
	}
	slices.Sort(merged)
	merged = slices.Compact(merged)
	for _, group := range *a {
		if slices.Equal(group, merged) {
			return false
		}
	}
	*a = append(keep, merged)
	return true
}

// aliasesOf reads stored groups ([[a, b], …]); broken entries drop out.
func aliasesOf(raw any) Aliases {
	if a, ok := raw.(Aliases); ok {
		return a
	}
	list, _ := raw.([]any)
	var out Aliases
	for _, item := range list {
		names, _ := item.([]any)
		var group []string
		for _, n := range names {
			if s, ok := n.(string); ok && normalize(s) != "" {
				group = append(group, normalize(s))
			}
		}
		slices.Sort(group)
		if group = slices.Compact(group); len(group) >= 2 {
			out = append(out, group)
		}
	}
	return out
}
