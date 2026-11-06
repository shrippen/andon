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
//	"Reichelt"                     cut off by card "Paypal *reicheltele"
//	"Invoice Ninja"                cut off by card "Paypal *invoiceninj"

// plainWords say where a company sits, not which it is.
var plainWords = map[string]bool{"deutsche": true, "deutscher": true, "deutsches": true, "deutschland": true, "germany": true}

// Least lengths: a word of a name; a whole name to match a cut-off one.
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

// vendorScore is how much of the vendor's name the booking's words hold,
// 0 without its brand.
func vendorScore(name string, booking []string) int {
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
	for _, w := range booking {
		cut := len(w) > len(own[0]) && (strings.HasPrefix(whole, w) || strings.HasPrefix(w, whole))
		if cut && min(len(w), len(whole)) >= minCutOff {
			return len(own)
		}
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
func namesVendor(name, text string) bool { return vendorScore(name, vendorWords(text)) > 0 }

// vendorOf is the vendor a booking names: its exact name, else the one
// with the most of its name in the text.
func vendorOf(n *sources.NinjaDataset, payer, text string) (sources.NinjaVendor, bool) {
	exact := strings.Join(strings.Fields(payer), " ")
	booking := vendorWords(text)
	var best sources.NinjaVendor
	top := 0
	for _, v := range n.Vendors {
		if strings.EqualFold(v.Name, exact) {
			return v, true
		}
		if s := vendorScore(v.Name, booking); s > top {
			best, top = v, s
		}
	}
	return best, top > 0
}
