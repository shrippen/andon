package receipts

// Fuzzy name comparison, as rapidfuzz does it (0–100):
//
//	ratio          2·LCS / (len a + len b), on runes
//	tokenSetRatio  words in common count fully: "acme gmbh" vs "acme" → 100
//	partialRatio   the shorter string against every window of the longer:
//	               "hetzner" in "rechnung hetzner online" → 100

import (
	"slices"
	"strings"
	"unicode"
)

const full = 100

// normalize lower-cases a name and keeps only letters and digits:
// "ACME GmbH & Co." → "acme gmbh co".
func normalize(name string) string {
	clean := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return ' '
	}, name)
	return strings.Join(strings.Fields(clean), " ")
}

// ratio is the similarity of two strings in percent.
func ratio(a, b string) float64 {
	ra, rb := []rune(a), []rune(b)
	total := len(ra) + len(rb)
	if total == 0 {
		return full
	}
	return float64(2*lcs(ra, rb)) * full / float64(total)
}

// lcs is the length of the longest common subsequence.
func lcs(a, b []rune) int {
	prev, cur := make([]int, len(b)+1), make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			switch {
			case a[i-1] == b[j-1]:
				cur[j] = prev[j-1] + 1
			case prev[j] >= cur[j-1]:
				cur[j] = prev[j]
			default:
				cur[j] = cur[j-1]
			}
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

// tokenSetRatio compares the word sets: shared words, then each side's
// extra words appended, best of the three pairings.
func tokenSetRatio(a, b string) float64 {
	wa, wb := wordSet(a), wordSet(b)
	var common, onlyA, onlyB []string
	for w := range wa {
		if wb[w] {
			common = append(common, w)
		} else {
			onlyA = append(onlyA, w)
		}
	}
	for w := range wb {
		if !wa[w] {
			onlyB = append(onlyB, w)
		}
	}
	if len(common) > 0 && (len(onlyA) == 0 || len(onlyB) == 0) {
		return full
	}

	join := func(parts ...[]string) string {
		var all []string
		for _, p := range parts {
			slices.Sort(p)
			all = append(all, p...)
		}
		return strings.Join(all, " ")
	}
	sect, withA, withB := join(common), join(common, onlyA), join(common, onlyB)
	best := ratio(withA, withB)
	if sect != "" {
		best = max(best, ratio(sect, withA), ratio(sect, withB))
	}
	return best
}

func wordSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, w := range strings.Fields(s) {
		out[w] = true
	}
	return out
}

// partialRatio is the best ratio of the shorter string against any
// window of the longer one, windows cut off at either end included.
func partialRatio(a, b string) float64 {
	short, long := []rune(a), []rune(b)
	if len(short) > len(long) {
		short, long = long, short
	}
	if len(short) == 0 {
		return 0
	}
	best := 0.0
	for start := 1 - len(short); start < len(long); start++ {
		window := long[max(start, 0):min(start+len(short), len(long))]
		score := float64(2*lcs(short, window)) * full / float64(len(short)+len(window))
		if score > best {
			best = score
		}
		if best == full {
			break
		}
	}
	return best
}
