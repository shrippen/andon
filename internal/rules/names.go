package rules

import (
	"sort"
	"strconv"
	"strings"
)

// aged is something named that has waited a number of days (an idle
// device, a silent feed).
type aged struct {
	name string
	days int
}

// agedNames lists the longest waiting first, same names counted once:
// "Akku, Patchkabel (10×), Stativ +4". It sorts items in place.
func agedNames(items []aged) string {
	sort.SliceStable(items, func(i, j int) bool { return items[i].days > items[j].days })
	count := map[string]int{}
	var order []string
	for _, a := range items {
		if count[a.name] == 0 {
			order = append(order, a.name)
		}
		count[a.name]++
	}
	shown := make([]string, 0, listShown)
	for _, name := range order {
		if len(shown) == listShown {
			break
		}
		if n := count[name]; n > 1 {
			name += " (" + strconv.Itoa(n) + "×)"
		}
		shown = append(shown, name)
	}
	out := strings.Join(shown, ", ")
	if rest := len(order) - len(shown); rest > 0 {
		out += " +" + strconv.Itoa(rest)
	}
	return out
}
