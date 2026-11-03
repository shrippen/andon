package bayes

import (
	"encoding/json"
	"testing"
)

// It learns which words go with which label, ignores unknown ones and
// proposes nothing without evidence.
func TestModel(t *testing.T) {
	m := New()
	if _, ok := m.Best([]string{"hetzner"}, 0); ok {
		t.Fatal("an empty model guesses")
	}
	m.Learn("1", "EDV", []string{"hetzner", "server"})
	m.Learn("2", "EDV", []string{"kagi", "suche"})
	m.Learn("3", "EDV", []string{"hetzner", "cloud"})
	m.Learn("4", "Reise", []string{"teilauto", "fahrt"})
	m.Learn("5", "", []string{"ignored"})

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

// A corrected example replaces its old label; a stored model guesses as
// before; learning the same again changes nothing.
func TestRelearnAndStore(t *testing.T) {
	m := New()
	m.Learn("1", "EDV", []string{"teilauto"})
	m.Learn("2", "Reise", []string{"bahn"})
	if !m.Learn("1", "Reise", []string{"teilauto"}) || m.Learn("1", "Reise", []string{"teilauto"}) {
		t.Fatal("relearn must change once")
	}
	if s := m.Stats(); s.Examples != 2 || s.Labels != 1 || s.Features != 2 {
		t.Fatalf("stats %+v", s)
	}

	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	back := New()
	if err := json.Unmarshal(b, back); err != nil {
		t.Fatal(err)
	}
	g, ok := back.Best([]string{"teilauto"}, 0.5)
	if !ok || g.Label != "Reise" || back.Stats() != m.Stats() {
		t.Fatalf("stored: %+v %v %+v", g, ok, back.Stats())
	}
}

// A model that knows one label only cannot tell anything apart: every
// known feature gives p = 1. It proposes nothing until a second label
// has examples.
func TestUseNeedsTwoLabels(t *testing.T) {
	m := New()
	m.Learn("1", "other", []string{"ihre", "newsletter"})
	u := Use{Model: m, MinP: 0.99}
	evidence := func(string) bool { return true }
	if g, ok := u.Best([]string{"ihre", "rechnung"}, evidence); ok {
		t.Fatalf("one label proposes %+v", g)
	}
	m.Learn("2", "invoice", []string{"rechnung"})
	u.MinP = 0.8
	if _, ok := u.Best([]string{"newsletter"}, evidence); !ok {
		t.Fatal("two labels propose nothing")
	}
}
