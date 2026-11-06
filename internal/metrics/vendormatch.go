package metrics

import (
	"slices"
	"strings"

	"andon/internal/sources"
)

// A booking names a vendor by its brand: the name's first long word that
// tells it apart. Short words are common in any text ("am", "on", "11",
// "St."), so a name of short words needs all of them, the first at the
// start of the payer.
//
//	"Dr. Müller Steuerberatung"    brand mueller  not "Dr. Schmidt"
//	"Deutsche Bahn AG"             brand bahn     not "Deutsche Kreditbank"
//	"OBI GmbH Co. Deutschland KG"  brand obi      "OBI Markt", not "netcup GmbH"
//	"Reichelt"                     cut off by a card feed: "Paypal *reicheltele"
//
// A long plain first word still decides ("Stadtwerke Bonn" ~ "Stadtwerke
// Köln"), as no list can name them all; the known ones stand in plainWords.

// plainWords are long words that say what or where a company is, not which.
var plainWords = map[string]bool{"deutsche": true, "deutscher": true, "deutsches": true, "deutschland": true, "germany": true,
	"prof": true, "invoice": true, "rechnung": true}

// Least lengths: a word to compare; a whole name to match a cut-off one.
const (
	minVendorWord = 2
	minCutOff     = 6
)

// spelledOut writes umlauts as bank feeds do: "müller" ~ "MUeLLER".
var spelledOut = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss")

// vendorWords are the words of a name or booking, lower case.
func vendorWords(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(spelledOut.Replace(strings.ToLower(s)), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		if len(w) >= minVendorWord {
			out = append(out, w)
		}
	}
	return out
}

// cardName is the word a card feed cut off, after its last "*":
// "VISA *PAYPAL *reicheltele"; "" without one.
func cardName(text string) string {
	i := strings.LastIndex(text, "*")
	if i < 0 {
		return ""
	}
	if words := vendorWords(text[i+1:]); len(words) > 0 {
		return words[0]
	}
	return ""
}

// bookingText is what a booking says for vendorScore.
type bookingText struct {
	words []string
	first string // the payer's first word
	card  string // the word a card feed cut off
}

func bookingOf(payer, text string) bookingText {
	b := bookingText{words: vendorWords(text), card: cardName(text)}
	if w := vendorWords(payer); len(w) > 0 {
		b.first = w[0]
	}
	return b
}

// vendorBrand is a name's brand and all its telling words; short says the
// brand is a short word.
func vendorBrand(name string) (brand string, own []string, short bool) {
	shortest := ""
	for _, w := range vendorWords(name) {
		if legalForms[w] {
			continue
		}
		own = append(own, w)
		switch {
		case plainWords[w]:
		case len(w) >= minNameLen && brand == "":
			brand = w
		case shortest == "":
			shortest = w
		}
	}
	if brand != "" {
		return brand, own, false
	}
	return shortest, own, true
}

// vendorScore is how much of the vendor's name the booking holds, 0
// without its brand; a whole name for the card feed's cut-off one.
func vendorScore(name string, b bookingText) int {
	brand, own, short := vendorBrand(name)
	if brand == "" {
		return 0
	}

	// A card feed cuts or runs names together: "reicheltele", "invoiceninj".
	whole := strings.Join(own, "")
	cut := len(b.card) > len(own[0]) && (strings.HasPrefix(whole, b.card) || strings.HasPrefix(b.card, whole))
	if cut && min(len(b.card), len(whole)) >= minCutOff {
		return len(own)
	}

	if !slices.Contains(b.words, brand) || short && (b.first != brand || !holdsAll(b.words, own)) {
		return 0
	}
	score := 0
	for _, w := range own {
		if slices.Contains(b.words, w) {
			score++
		}
	}
	return score
}

// holdsAll says whether the booking has every telling word of a name made
// of short words: "Dr. Ott" is not "Dr. Schmidt".
func holdsAll(booking, own []string) bool {
	for _, w := range own {
		if !plainWords[w] && !slices.Contains(booking, w) {
			return false
		}
	}
	return true
}

// namesVendor says whether the text, starting with the payer, names the vendor.
func namesVendor(name, text string) bool { return vendorScore(name, bookingOf(text, text)) > 0 }

// vendorOf is the vendor a booking names: its exact name, else the one
// with the most of its name in the text.
func vendorOf(n *sources.NinjaDataset, payer, text string) (sources.NinjaVendor, bool) {
	exact := strings.Join(strings.Fields(payer), " ")
	b := bookingOf(payer, text)
	var best sources.NinjaVendor
	top := 0
	for _, v := range n.Vendors {
		if strings.EqualFold(v.Name, exact) {
			return v, true
		}
		if s := vendorScore(v.Name, b); s > top {
			best, top = v, s
		}
	}
	return best, top > 0
}
