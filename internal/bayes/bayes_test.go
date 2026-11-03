package bayes

import "testing"

// It learns which words go with which label, ignores unknown ones and
// proposes nothing without evidence.
func TestModel(t *testing.T) {
	m := New()
	if _, ok := m.Best([]string{"hetzner"}, 0); ok {
		t.Fatal("an empty model guesses")
	}
	m.Learn("EDV", []string{"hetzner", "server"})
	m.Learn("EDV", []string{"kagi", "suche"})
	m.Learn("EDV", []string{"hetzner", "cloud"})
	m.Learn("Reise", []string{"teilauto", "fahrt"})
	m.Learn("", []string{"ignored"})

	g, ok := m.Best([]string{"hetzner", "neu", "rechnung"}, 0.5)
	if !ok || g.Label != "EDV" || g.P < 0.8 {
		t.Fatalf("hetzner: %+v %v", g, ok)
	}
	g, ok = m.Best([]string{"teilauto"}, 0.5)
	if !ok || g.Label != "Reise" {
		t.Fatalf("teilauto: %+v %v", g, ok)
	}
	if _, ok := m.Best([]string{"unbekannt", "ignored"}, 0); ok {
		t.Fatal("unknown features guess from the prior")
	}

	// Probabilities add up to one.
	var sum float64
	for _, r := range m.Rank([]string{"server", "fahrt"}) {
		sum += r.P
	}
	if sum < 0.999 || sum > 1.001 {
		t.Fatalf("sum %v", sum)
	}
}
