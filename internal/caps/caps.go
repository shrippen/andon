// Package caps says what each integration can do in a domain Andon
// shares between services (places, rides, customers …), and picks who
// stores a value when several could:
//
//	declared (here)            detected (sources)          decided (services)
//	Kimai: places update  ──►  ping: placesWrite ✓  ──►    Store(places, update,
//	  needs plugin mileage,    editOwn ✗                   "private", order)
//	  feature placesWrite,     ⇒ Set{Have: read,           → Andon
//	  right editOwn              Missing: update (editOwn)}
//
// A leaf package (it imports only enums): every layer may use it.
package caps

import (
	"slices"

	"andon/internal/enums"
)

// Domain is a topic several services know.
type Domain string

const (
	Places        Domain = "places"
	Rides         Domain = "rides"
	Customers     Domain = "customers"
	Absences      Domain = "absences"
	WorkTime      Domain = "worktime"
	Invoices      Domain = "invoices"
	Payments      Domain = "payments"
	Receipts      Domain = "receipts"
	Appointments  Domain = "appointments"
	Subscriptions Domain = "subscriptions"
)

// Op is what a service does with a domain.
type Op string

const (
	Read   Op = "read"
	Create Op = "create"
	Update Op = "update"
)

// NeedKind is what a connection must have for a capability.
type NeedKind string

const (
	NeedPlugin  NeedKind = "plugin"  // a server plugin, e.g. Kimai's mileage
	NeedFeature NeedKind = "feature" // a feature of it, e.g. placesWrite
	NeedRight   NeedKind = "right"   // a right of the token, e.g. editOwn
	NeedAPI     NeedKind = "api"     // an endpoint newer versions have, e.g. tracks
	NeedSetting NeedKind = "setting" // a value kept in the service, e.g. Kimai's working time
)

// Need is one requirement, e.g. {right editOwn}.
type Need struct {
	Kind NeedKind
	Name string
}

// Holder is who can keep a value: a service, or Andon itself.
type Holder string

// Andon keeps what no service can.
const Andon Holder = "andon"

// HolderOf is the holder of a service.
func HolderOf(s enums.ServiceType) Holder { return Holder(s) }

// Use is a domain of a holder, e.g. Dawarich's places.
type Use struct {
	Holder Holder
	Domain Domain
}

// Cap is one thing a holder can do in a domain.
type Cap struct {
	Domain Domain
	Op     Op
	Kinds  []string // variants it supports (place kinds, ride modes); nil = all
	Needs  []Need
	// Refs are other holders' domains whose ids it stores: a mileage
	// plugin place keeps its Dawarich area, so the area comes first.
	Refs []Use
}

// Fits reports whether the capability covers kind ("" = any).
func (c Cap) Fits(kind string) bool {
	return kind == "" || c.Kinds == nil || slices.Contains(c.Kinds, kind)
}

// Same reports whether c and o are the same declared capability.
func (c Cap) Same(o Cap) bool {
	return c.Domain == o.Domain && c.Op == o.Op && slices.Equal(c.Kinds, o.Kinds)
}

// Gap is a declared capability a connection lacks, and the first need
// it misses.
type Gap struct {
	Cap  Cap
	Need Need
}

// Set is what one connection can do now.
type Set struct {
	Holder  Holder
	Have    []Cap
	Missing []Gap
}

// Can reports whether the set holds op on kind of a domain.
func (s Set) Can(d Domain, op Op, kind string) bool {
	for _, c := range s.Have {
		if c.Domain == d && c.Op == op && c.Fits(kind) {
			return true
		}
	}
	return false
}

// Lacks returns why op on a domain is missing, false if it is not
// declared or not missing.
func (s Set) Lacks(d Domain, op Op) (Need, bool) {
	for _, g := range s.Missing {
		if g.Cap.Domain == d && g.Cap.Op == op {
			return g.Need, true
		}
	}
	return Need{}, false
}

// Gap returns the gap of a declared capability, false if the set has it.
func (s Set) Gap(c Cap) (Gap, bool) {
	for _, g := range s.Missing {
		if g.Cap.Same(c) {
			return g, true
		}
	}
	return Gap{}, false
}

// Partial lists the gaps in domains the connection serves otherwise:
// "places read, but not update" is worth a note, a plugin that is not
// installed at all is not.
func (s Set) Partial() []Gap {
	var out []Gap
	for _, g := range s.Missing {
		if slices.ContainsFunc(s.Have, func(c Cap) bool { return c.Domain == g.Cap.Domain }) {
			out = append(out, g)
		}
	}
	return out
}

// Detect sorts the holder's declared capabilities by met: e.g. the
// Kimai source answers met({right editOwn}) from the plugin's ping.
func Detect(h Holder, met func(Need) bool) Set {
	out := Set{Holder: h}
	for _, c := range Declared(h) {
		missing, ok := firstUnmet(c.Needs, met)
		if ok {
			out.Missing = append(out.Missing, Gap{Cap: c, Need: missing})
			continue
		}
		out.Have = append(out.Have, c)
	}
	return out
}

func firstUnmet(needs []Need, met func(Need) bool) (Need, bool) {
	for _, n := range needs {
		if !met(n) {
			return n, true
		}
	}
	return Need{}, false
}

// Full is everything the holder declares, e.g. Andon's own set.
func Full(h Holder) Set {
	return Detect(h, func(Need) bool { return true })
}

// Store picks who keeps op on kind of a domain: the first set in order
// that can, else Andon. Example: places, update, "private" with
// [Kimai] → Andon, as the mileage plugin has no private places.
func Store(d Domain, op Op, kind string, order ...Set) Holder {
	for _, s := range order {
		if s.Can(d, op, kind) {
			return s.Holder
		}
	}
	return Andon
}

// Reporter is a dataset that knows its connection's capabilities.
type Reporter interface{ CapSet() Set }

// RefsOf lists the domains a holder's capabilities in d refer to.
func RefsOf(h Holder, d Domain) []Use {
	var out []Use
	for _, c := range Declared(h) {
		if c.Domain != d {
			continue
		}
		for _, r := range c.Refs {
			if !slices.Contains(out, r) {
				out = append(out, r)
			}
		}
	}
	return out
}

// Needed lists the holders a reader of uses depends on: their own and,
// through their references, the holders whose ids they keep (Kimai's
// places need Dawarich's).
func Needed(uses ...Use) []Holder {
	var out []Holder
	seen := map[Use]bool{}
	var walk func(u Use)
	walk = func(u Use) {
		if seen[u] {
			return
		}
		seen[u] = true
		if !slices.Contains(out, u.Holder) {
			out = append(out, u.Holder)
		}
		for _, r := range RefsOf(u.Holder, u.Domain) {
			walk(r)
		}
	}
	for _, u := range uses {
		walk(u)
	}
	return out
}

// Order sorts holders that write in domain d so that each comes after
// the holders whose ids it stores (Dawarich's area before Kimai's
// place). Holders that refer to each other keep their given order.
func Order(d Domain, holders ...Holder) []Holder {
	out := slices.Clone(holders)
	refers := func(a, b Holder) bool { return slices.Contains(RefsOf(a, d), Use{b, d}) }
	// At most n² swaps: a longer cycle (a → b → c → a) stops there.
	swaps := len(out) * len(out)
	for i := 0; i < len(out) && swaps > 0; i++ {
		for j := i + 1; j < len(out); j++ {
			if refers(out[i], out[j]) && !refers(out[j], out[i]) {
				out[i], out[j] = out[j], out[i]
				swaps--
				i = -1 // restart: a swap can unsort what came before
				break
			}
		}
	}
	return out
}
