package scheduler

import (
	"testing"
	"time"
)

// TestNextWaitSpreads: runs drift a little around the interval, so jobs
// with equal intervals stop hitting services at the same moment.
func TestNextWaitSpreads(t *testing.T) {
	const interval = 10 * time.Minute
	low, high := interval-interval/jitterShare/2, interval+interval/jitterShare/2

	seen := map[time.Duration]bool{}
	for range 200 {
		wait := nextWait(interval)
		if wait < low || wait > high {
			t.Fatalf("wait %v outside [%v, %v]", wait, low, high)
		}
		seen[wait] = true
	}
	if len(seen) < 2 {
		t.Fatal("expected varying waits")
	}
}
