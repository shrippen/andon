package analysis

import "testing"

// A connection is reported down only when two runs in a row failed; one
// slow answer is no outage. A success resets the count.
func TestDownAfterTwoFailures(t *testing.T) {
	conn := fetcher{conn: 4711}
	steps := []struct {
		ok, want bool
	}{{false, false}, {false, true}, {false, true}, {true, false}, {false, false}}
	for i, s := range steps {
		if got := reportDown(conn, s.ok); got != s.want {
			t.Fatalf("step %d: %v", i, got)
		}
	}
}
