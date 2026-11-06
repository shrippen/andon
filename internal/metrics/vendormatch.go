package metrics

import (
	"slices"
	"strings"
	"unicode"

	"andon/internal/sources"
)

// A booking names a vendor by its brand: the name's first long word that
// tells it apart. Short words are common in any text ("am", "on", "11",
// "St."), so a name of short words must open the payer, word by word.
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

// allWords are the words of a name or booking, lower case.
func allWords(s string) []string {
	return strings.FieldsFunc(spelledOut.Replace(strings.ToLower(s)), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// vendorWords are the words to compare: no lone letters.
func vendorWords(s string) []string {
	return slices.DeleteFunc(allWords(s), func(w string) bool { return len(w) < minVendorWord })
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
	payer []string
	card  string // the word a card feed cut off
}

func bookingOf(payer, text string) bookingText {
	return bookingText{words: vendorWords(text), payer: vendorWords(payer), card: cardName(text)}
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

	// Short words alone must be the whole name: "Dr. O." is not "Dr.".
	if slices.ContainsFunc(allWords(name), func(w string) bool { return len(w) < minVendorWord }) {
		return "", own, true
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

	if short {
		if !opensPayer(b.payer, own) {
			return 0
		}
		return len(own)
	}
	if !slices.Contains(b.words, brand) {
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

// opensPayer says whether the payer starts with a name of short words,
// its last plain words aside ("OBI GmbH Co. Deutschland KG" is "OBI"):
// "Dr. Ott" is not "Dr. Schmidt Ott".
func opensPayer(payer, own []string) bool {
	for len(own) > 0 && plainWords[own[len(own)-1]] {
		own = own[:len(own)-1]
	}
	return len(own) > 0 && len(payer) >= len(own) && slices.Equal(payer[:len(own)], own)
}

// namesVendor says whether the booking names the vendor.
func namesVendor(name, payer, text string) bool {
	return vendorScore(name, bookingOf(payer, text)) > 0
}

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
