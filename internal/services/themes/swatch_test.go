package themes

import (
	"testing"

	"andon/internal/model"
)

// Swatches follow var() references: Kante's --primary is its yellow.
func TestSwatches(t *testing.T) {
	got := swatches(&model.Theme{Dark: map[string]any{"--primary": "var(--yellow)", "--yellow": "#fabd2f"}})
	if len(got) != len(swatchTokens) || got[2] != "#fabd2f" {
		t.Fatalf("swatches: %v", got)
	}
}
