package metrics

import (
	"math/rand/v2"
	"testing"
	"time"
)

// TestCoversMatchScan: the sorted index finds what a scan over all
// spans finds, also with overlapping spans and ties (the first given
// wins).
func TestCoversMatchScan(t *testing.T) {
	scan := func(spans []span, a, b time.Time) (span, bool) {
		total := b.Sub(a)
		if total <= 0 {
			return span{}, false
		}
		var best span
		var bestD, sum time.Duration
		for _, s := range spans {
			d := s.overlap(a, b)
			sum += d
			if d > bestD {
				best, bestD = s, d
			}
		}
		return best, bestD > 0 && float64(sum) >= coverShare*float64(total)
	}

	r := rand.New(rand.NewPCG(1, 2))
	base := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	minute := func(n int) time.Time { return base.Add(time.Duration(n) * time.Minute) }
	for round := range 200 {
		var spans []span
		for i := range r.IntN(30) {
			from := r.IntN(600) / 10 * 10 // on the 10 minutes: ties
			spans = append(spans, span{from: minute(from), to: minute(from + r.IntN(120)), customer: int64(i)})
		}
		c := coversOf(spans)
		for range 50 {
			a := r.IntN(700) - 50
			b := a + r.IntN(90) - 5
			want, wantOK := scan(spans, minute(a), minute(b))
			got, ok := c.best(minute(a), minute(b))
			if ok != wantOK || (ok && got.customer != want.customer) {
				t.Fatalf("round %d [%d, %d): got %d %v, want %d %v", round, a, b, got.customer, ok, want.customer, wantOK)
			}
		}
	}
}
