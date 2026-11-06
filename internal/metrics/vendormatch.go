package metrics

import (
	"slices"
	"strings"

	"andon/internal/sources"
)

// A booking names a vendor by its brand: the first word of the vendor's
// name that tells it apart, short ones too. Words every company has say
// nothing.
//
//	"OBI GmbH Co. Deutschland KG"  brand obi       not "netcup GmbH"
//	"Deutsche Bahn AG"             brand bahn      not "Deutsche Kreditbank"
//	"Dr. Müller Steuerberatung"    brand mueller   not "Dr. Schmidt"
//	"Telekom"                                      not "Bechtle Telekommunikation"
//	"Reichelt"                     cut off by card "Paypal *reicheltele"
//	"Invoice Ninja"                cut off by card "Paypal *invoiceninj"

// plainWords say where a company sits or how it is called, not which it is.
var plainWords = map[string]bool{"deutsche": true, "deutscher": true, "deutsches": true, "deutschland": true, "germany": true,
	"dr": true, "prof": true, "the": true, "der": true, "die": true, "das": true, "zum": true, "zur": true, "www": true, "com": true, "de": true,
	"invoice": true, "rechnung": true}

// Least lengths: a word of a name; a whole name to match a cut-off one.
const (
	minVendorWord = 2
	minCutOff     = 6
)

// spelledOut writes umlauts as bank feeds do: "müller" ~ "MUeLLER".
var spelledOut = strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss")

// vendorWords are the words of a name or booking, lower case; a lone
// letter belongs to the next: "E.ON" is "eon", "H&M" "hm".
func vendorWords(s string) []string {
	var out []string
	letter := ""
	for _, w := range strings.FieldsFunc(spelledOut.Replace(strings.ToLower(s)), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	}) {
		w = letter + w
		letter = ""
		if len(w) < minVendorWord {
			letter = w
			continue
		}
		out = append(out, w)
	}
	return out
}

// cardName is the word a card feed cut off, after its "*":
// "Paypal *reicheltele"; "" without one.
func cardName(text string) string {
	_, after, found := strings.Cut(text, "*")
	if words := vendorWords(after); found && len(words) > 0 {
		return words[0]
	}
	return ""
}

// vendorScore is how much of the vendor's name the booking's words hold,
// 0 without its brand; a whole name for the card feed's cut-off one.
func vendorScore(name string, booking []string, card string) int {
	var own []string
	brand := ""
	for _, w := range vendorWords(name) {
		if legalForms[w] {
			continue
		}
		own = append(own, w)
		if brand == "" && !plainWords[w] {
			brand = w
		}
	}
	if brand == "" {
		return 0
	}

	// A card feed cuts or runs names together: "reicheltele", "invoiceninj".
	whole := strings.Join(own, "")
	cut := len(card) > len(own[0]) && (strings.HasPrefix(whole, card) || strings.HasPrefix(card, whole))
	if cut && min(len(card), len(whole)) >= minCutOff {
		return len(own)
	}
	if !slices.Contains(booking, brand) {
		return 0
	}
	score := 0
	for _, w := range own {
		if slices.Contains(booking, w) {
			score++
		}
	}
	return score
}

// namesVendor says whether the text names the vendor.
func namesVendor(name, text string) bool {
	return vendorScore(name, vendorWords(text), cardName(text)) > 0
}

// vendorOf is the vendor a booking names: its exact name, else the one
// with the most of its name in the text.
func vendorOf(n *sources.NinjaDataset, payer, text string) (sources.NinjaVendor, bool) {
	exact := strings.Join(strings.Fields(payer), " ")
	booking, card := vendorWords(text), cardName(payer)
	var best sources.NinjaVendor
	top := 0
	for _, v := range n.Vendors {
		if strings.EqualFold(v.Name, exact) {
			return v, true
		}
		if s := vendorScore(v.Name, booking, card); s > top {
			best, top = v, s
		}
	}
	return best, top > 0
}
