package receipts

import (
	"math"
	"testing"
)

// Values checked against rapidfuzz 3 (fuzz.ratio, token_set_ratio,
// partial_ratio).
func TestFuzzy(t *testing.T) {
	cases := []struct {
		name string
		got  float64
		want float64
	}{
		{"ratio", ratio("this is a test", "this is a test!"), 96.55},
		{"ratio empty", ratio("", "abc"), 0},
		{"token set subset", tokenSetRatio("acme gmbh", "acme"), 100},
		{"token set reordered", tokenSetRatio("hetzner online gmbh", "gmbh hetzner online"), 100},
		{"token set partial", tokenSetRatio("hetzner cloud", "hetzner online"), 70},
		{"token set weak", tokenSetRatio("bürobedarf schmidt", "quittung schmidt"), 60.87},
		{"token set none", tokenSetRatio("deutsche bahn", "db fernverkehr ag"), 33.33},
		{"partial weak", partialRatio("bürobedarf schmidt", "quittung schmidt"), 66.67},
		{"partial none", partialRatio("deutsche bahn", "db fernverkehr ag"), 42.11},
		{"partial shifted", partialRatio("hetzner online", "rechnung hetzner"), 66.67},
		{"partial inside", partialRatio("hetzner", "rechnung hetzner online"), 100},
		{"partial empty", partialRatio("xyz", "abc"), 0},
	}
	for _, c := range cases {
		if math.Abs(c.got-c.want) > 0.01 {
			t.Errorf("%s: %.2f, want %.2f", c.name, c.got, c.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	if got := normalize("  ACME GmbH & Co. KG, Bürobedarf "); got != "acme gmbh co kg bürobedarf" {
		t.Fatalf("normalize: %q", got)
	}
}

func TestAliasesLearn(t *testing.T) {
	var a Aliases
	if !a.learn("Hetzner", "Hetzner Online GmbH") || a.learn("hetzner", "HETZNER ONLINE GMBH") {
		t.Fatal("learn once")
	}
	a.learn("Hetzner Online GmbH", "Hetzner Cloud")
	a.learn("Lenovo", "Lenovo Deutschland")
	if len(a) != 2 || len(a.equivalents("hetzner cloud")) != 3 {
		t.Fatalf("groups: %v", a)
	}
	if got := aliasesOf([]any{[]any{"A", "a"}, []any{"B", "c"}, "junk"}); len(got) != 1 {
		t.Fatalf("stored: %v", got)
	}
}
