package bayes

import (
	"encoding/json"
	"slices"
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

// Prune forgets the examples it names, and only those.
func TestPrune(t *testing.T) {
	m := New()
	m.Learn("a:1", "x", []string{"w"})
	m.Learn("b:1", "y", []string{"v"})
	if !m.Prune(func(id string) bool { return id == "a:1" }) || m.Stats().Examples != 1 || m.Knows("w") || !m.Knows("v") {
		t.Fatalf("prune: %+v", m.Stats())
	}
	if m.Prune(func(string) bool { return false }) {
		t.Fatal("nothing pruned, changed")
	}
}

// Suggest lists the likely labels, best first: the best must reach
// MinP, the others a third of it; at most max; nothing without evidence
// or with one label. Here: EDV 0.50, GWG 0.33, Reise 0.17.
func TestSuggest(t *testing.T) {
	m := New()
	for i, l := range []string{"EDV", "EDV", "EDV", "GWG", "GWG", "Reise"} {
		m.Learn(string(rune('a'+i)), l, []string{"server"})
	}
	m.Learn("z", "Reise", []string{"bahn"})
	evidence := func(string) bool { return true }
	labels := func(gs []Guess) (out []string) {
		for _, g := range gs {
			out = append(out, g.Label)
		}
		return out
	}

	cases := []struct {
		minP float64
		max  int
		want []string
	}{
		{0.4, 3, []string{"EDV", "GWG", "Reise"}}, // others from 0.13
		{0.4, 2, []string{"EDV", "GWG"}},
		{0.6, 3, nil}, // the best is below MinP
	}
	for _, c := range cases {
		got := labels(Use{Model: m, MinP: c.minP}.Suggest([]string{"server"}, evidence, c.max))
		if !slices.Equal(got, c.want) {
			t.Errorf("MinP %.2f max %d: %v, want %v", c.minP, c.max, got, c.want)
		}
	}
	if got := (Use{Model: m, MinP: 0.4}).Suggest([]string{"unbekannt"}, evidence, 3); got != nil {
		t.Fatalf("no evidence: %+v", got)
	}
}
