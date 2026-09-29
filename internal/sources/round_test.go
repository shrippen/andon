package sources

import "testing"

// Negative amounts (credits, refunds) round away from zero like positive ones.
func TestRound2Negative(t *testing.T) {
	if got := round2(-1.236); got != -1.24 {
		t.Fatalf("round2(-1.236) = %v, want -1.24", got)
	}
}
