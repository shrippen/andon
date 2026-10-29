package metrics

// Subscriptions across services: Sure sees what the bank pays, Wallos
// lists the subscriptions, Invoice Ninja books the business ones as
// recurring expenses. The bench (services/subs) brings the other two in
// line with the bank:
//
//	Sure recurring ──┬─ not in Wallos ───────────► add to Wallos
//	                 ├─ not in Ninja (business) ──► recurring expense
//	                 ├─ other price ──────────────► new price there
//	                 └─ ended in Sure ────────────► end it there
//	Wallos or Ninja twice ────────────────────────► remove the second

import (
	"cmp"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/sources"
)

// SubAction is what a case proposes.
type SubAction string

const (
	SubWallosAdd   SubAction = "wallos_add"
	SubWallosPrice SubAction = "wallos_price"
	SubWallosEnd   SubAction = "wallos_end"
	SubWallosMerge SubAction = "wallos_merge"
	SubNinjaAdd    SubAction = "ninja_add"
	SubNinjaPrice  SubAction = "ninja_price"
	SubNinjaEnd    SubAction = "ninja_end"
	SubNinjaMerge  SubAction = "ninja_merge"
)

// Case key prefixes, one per action: "wp-3" is Wallos subscription 3.
var subPrefix = map[SubAction]string{
	SubWallosAdd: "wa-", SubWallosPrice: "wp-", SubWallosEnd: "we-", SubWallosMerge: "wm-",
	SubNinjaAdd: "na-", SubNinjaPrice: "np-", SubNinjaEnd: "ne-", SubNinjaMerge: "nm-",
}

const (
	// SubMaxMonthly keeps rent and insurance out of Wallos: a payment
	// above it a month is no subscription (cross.wallos_missing's default).
	SubMaxMonthly = 100.0
	// subPriceTol is the difference that counts as a new price.
	subPriceTol = 0.01
	// subWeekDays: a shorter interval is weekly (0 months).
	subWeekDays  = 10
	avgMonthDays = 30.44
	// maxCatchUp bounds moving a past day forward (ten years of weeks).
	maxCatchUp = 520
)

// subMonths are the intervals the services share (Invoice Ninja's).
var subMonths = []int{1, 2, 3, 4, 6, 12, 24, 36}

// SubCase is one proposal.
type SubCase struct {
	Key    string
	Action SubAction
	Name   string
	Ticked bool // safe to write as proposed: a price the bank pays

	Bank   *sources.BankRecurring         // Sure's view, nil for a twin
	Wallos *sources.WallosSub             // the subscription it changes
	Ninja  *sources.NinjaRecurringExpense // the recurring expense it changes
	Keep   string                         // a twin's kept entry, by name and number

	Amount float64 // to write: the new price, a new entry's amount
	Old    float64 // the price there now
	Months int     // interval, 0 weekly
	Next   string  // next payment

	// A new recurring expense: its vendor (VendorKey "" = create Vendor),
	// category and tax, from the vendor's last expense.
	Vendor, VendorKey, CategoryKey, TaxName string
	TaxRate                                 float64
}

// SubInputs is what the cases are built from; Wallos and Ninja may be nil.
type SubInputs struct {
	Bank   *sources.BankData
	Wallos *sources.WallosDataset
	Ninja  *sources.NinjaDataset
	// Categories maps Sure's categories and tags to Ninja category keys
	// (Verbund → Kategorien).
	Categories sources.BankMap
	Today      time.Time
	// MaxMonthly and Ignore are cross.wallos_missing's settings: a
	// payment above MaxMonthly a month (0: SubMaxMonthly) or with an
	// ignored word in its name is not added to Wallos.
	MaxMonthly float64
	Ignore     []string
}

// SubCases lists the proposals, by action.
func SubCases(in SubInputs) []SubCase {
	var out []SubCase
	for i := range in.Bank.Recurring {
		r := &in.Bank.Recurring[i]
		if !r.Expense || r.Status == "inactive" {
			continue
		}
		months, ok := intervalMonths(*r)
		if !ok {
			continue
		}
		next := nextPayment(r.Next, months, in.Today)
		if in.Wallos != nil {
			out = append(out, wallosCases(in, r, months, next)...)
		}
		if in.Ninja != nil {
			out = append(out, ninjaCases(in, r, months, next)...)
		}
	}
	out = append(out, endedCases(in)...)
	out = append(out, twinCases(in)...)

	// One case per key: an entry two bank payments match is changed once.
	seen := map[string]bool{}
	out = slices.DeleteFunc(out, func(c SubCase) bool {
		dup := seen[c.Key]
		seen[c.Key] = true
		return dup
	})
	slices.SortStableFunc(out, func(a, b SubCase) int { return strings.Compare(string(a.Action), string(b.Action)) })
	return out
}

// wallosCases: r missing in Wallos, or there at another price.
func wallosCases(in SubInputs, r *sources.BankRecurring, months int, next string) []SubCase {
	w := in.Wallos
	var found []*sources.WallosSub
	for i := range w.Subs {
		if sameName(r.Name, w.Subs[i].Name) {
			found = append(found, &w.Subs[i])
		}
	}
	if len(found) == 0 {
		limit := cmp.Or(in.MaxMonthly, SubMaxMonthly)
		if monthly(*r) > limit || ignored(r.Name, in.Ignore) {
			return nil
		}
		return []SubCase{{Key: subKey(SubWallosAdd, r.Name), Action: SubWallosAdd, Name: r.Name, Bank: r, Amount: r.Amount, Months: months, Next: next}}
	}

	// The subscriptions the payment could change: running, same currency
	// and interval.
	var open []*sources.WallosSub
	for _, s := range found {
		if s.Inactive || s.CurrencyID != w.Main || s.Months() != months {
			continue
		}
		if math.Abs(s.Price-r.Amount) <= subPriceTol {
			return nil
		}
		open = append(open, s)
	}
	s, sure := closest(open, func(s *sources.WallosSub) float64 { return s.Price }, r.Amount)
	if s == nil {
		return nil
	}
	return []SubCase{{Key: subID(SubWallosPrice, itoa(s.ID)), Action: SubWallosPrice, Name: s.Name, Ticked: sure,
		Bank: r, Wallos: s, Amount: r.Amount, Old: s.Price, Months: months, Next: s.Next}}
}

// closest is the entry whose price lies nearest to amount; sure is
// false when there were several to choose from ("Netkom Internet" and
// "Netkom Handy" both match the payment "Netkom"), so the case is not
// ticked.
func closest[T any](entries []T, price func(T) float64, amount float64) (best T, sure bool) {
	if len(entries) == 0 {
		return best, false
	}
	best = entries[0]
	for _, e := range entries[1:] {
		if math.Abs(price(e)-amount) < math.Abs(price(best)-amount) {
			best = e
		}
	}
	return best, len(entries) == 1
}

// ninjaCases: a business payment without recurring expense, or one at
// another price.
func ninjaCases(in SubInputs, r *sources.BankRecurring, months int, next string) []SubCase {
	vendors := vendorNames(in.Ninja)
	var out []SubCase
	var open []*sources.NinjaRecurringExpense
	matched := false
	for i := range in.Ninja.RecurringExpenses {
		e := &in.Ninja.RecurringExpenses[i]
		if !sameName(r.Name, ninjaSubName(vendors, e)) {
			continue
		}
		matched = true
		if !e.Active || e.Months != months {
			continue
		}
		// No amount: a bill that varies (power), booked by hand.
		if e.Amount == 0 || math.Abs(e.Amount-r.Amount) <= subPriceTol {
			return nil
		}
		open = append(open, e)
	}
	if matched {
		e, sure := closest(open, func(e *sources.NinjaRecurringExpense) float64 { return e.Amount }, r.Amount)
		if e == nil {
			return nil
		}
		return []SubCase{{Key: subID(SubNinjaPrice, e.Key), Action: SubNinjaPrice, Name: subNinjaName(vendors, e), Ticked: sure,
			Bank: r, Ninja: e, Amount: r.Amount, Old: e.Amount, Months: months, Next: e.Next}}
	}
	if _, ok := sources.NinjaFrequency(months); !ok {
		return out
	}

	// Business: paid from a business or transfer account, or to a vendor
	// that has expenses (no role yet, or a mixed account).
	txn := lastPayment(in.Bank, r)
	vendor := subVendor(in.Ninja, r.Name)
	business := txn != nil && (txn.Role == sources.RoleBusiness || txn.Role == sources.RoleTransfer)
	if txn == nil || txn.Role == sources.RoleUnknown || txn.Role == sources.RoleMixed {
		_, known := lastExpense(in.Ninja, vendorKeyOf(vendor))
		business = known
	}
	if !business {
		return out
	}

	c := SubCase{Key: subKey(SubNinjaAdd, r.Name), Action: SubNinjaAdd, Name: r.Name, Bank: r, Amount: r.Amount, Months: months, Next: next, Vendor: r.Name}
	if vendor != nil {
		c.Vendor, c.VendorKey = vendor.Name, vendor.Key
		if e, ok := lastExpense(in.Ninja, vendor.Key); ok {
			c.CategoryKey, c.TaxName, c.TaxRate = e.CategoryKey, e.TaxName, e.TaxRate
		}
	}
	if c.CategoryKey == "" && txn != nil {
		c.CategoryKey = in.Categories.Key(txn.Category, txn.Tags)
	}
	return append(out, c)
}

// endedCases: Sure has stopped seeing a payment that Wallos or Ninja
// still run. Only an inactive Sure entry counts: a payment Sure never
// knew may come from an account it does not read.
func endedCases(in SubInputs) []SubCase {
	ended := func(name string) *sources.BankRecurring {
		var gone *sources.BankRecurring
		for i := range in.Bank.Recurring {
			r := &in.Bank.Recurring[i]
			if !r.Expense || !sameName(r.Name, name) {
				continue
			}
			if r.Status != "inactive" {
				return nil
			}
			gone = r
		}
		return gone
	}

	var out []SubCase
	if in.Wallos != nil {
		for i := range in.Wallos.Subs {
			s := &in.Wallos.Subs[i]
			if r := ended(s.Name); r != nil && !s.Inactive {
				out = append(out, SubCase{Key: subID(SubWallosEnd, itoa(s.ID)), Action: SubWallosEnd, Name: s.Name, Bank: r, Wallos: s, Old: s.Price, Next: s.Next})
			}
		}
	}
	if in.Ninja != nil {
		vendors := vendorNames(in.Ninja)
		for i := range in.Ninja.RecurringExpenses {
			e := &in.Ninja.RecurringExpenses[i]
			if r := ended(ninjaSubName(vendors, e)); r != nil && e.Active {
				out = append(out, SubCase{Key: subID(SubNinjaEnd, e.Key), Action: SubNinjaEnd, Name: subNinjaName(vendors, e), Bank: r, Ninja: e, Old: e.Amount, Next: e.Next})
			}
		}
	}
	return out
}

// twinCases: the same subscription entered twice (same name, price and
// interval); the first stays.
func twinCases(in SubInputs) []SubCase {
	var out []SubCase
	if in.Wallos != nil {
		seen := map[string]*sources.WallosSub{}
		for i := range in.Wallos.Subs {
			s := &in.Wallos.Subs[i]
			if s.Inactive {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(s.Name)) + "|" + money2(s.Price) + "|" + itoa(int64(s.Months()))
			if first, ok := seen[key]; ok {
				out = append(out, SubCase{Key: subID(SubWallosMerge, itoa(s.ID)), Action: SubWallosMerge, Name: s.Name, Wallos: s,
					Keep: first.Name + " #" + itoa(first.ID), Old: s.Price, Next: s.Next})
				continue
			}
			seen[key] = s
		}
	}
	if in.Ninja != nil {
		vendors := vendorNames(in.Ninja)
		seen := map[string]*sources.NinjaRecurringExpense{}
		for i := range in.Ninja.RecurringExpenses {
			e := &in.Ninja.RecurringExpenses[i]
			if !e.Active {
				continue
			}
			key := e.VendorKey + "|" + money2(e.Amount) + "|" + itoa(int64(e.Months))
			if first, ok := seen[key]; ok {
				out = append(out, SubCase{Key: subID(SubNinjaMerge, e.Key), Action: SubNinjaMerge, Name: subNinjaName(vendors, e), Ninja: e,
					Keep: subNinjaName(vendors, first), Old: e.Amount, Next: e.Next})
				continue
			}
			seen[key] = e
		}
	}
	return out
}

// sameName: one name's brand is a word of the other ("Adobe" ~ "Adobe
// Creative Cloud").
func sameName(a, b string) bool { return sameThing(a, b) || sameThing(b, a) }

// intervalMonths is r's interval (last → next) snapped to the services'
// intervals; 0 is weekly. Without both dates it is monthly.
func intervalMonths(r sources.BankRecurring) (int, bool) {
	last, ok1 := ParseDay(r.Last)
	next, ok2 := ParseDay(r.Next)
	if !ok1 || !ok2 {
		return 1, true
	}
	days := next.Sub(last).Hours() / hoursPerDay
	if days <= 0 {
		return 0, false
	}
	if days < subWeekDays {
		return 0, true
	}
	months := days / avgMonthDays
	best := subMonths[0]
	for _, m := range subMonths {
		if math.Abs(float64(m)-months) < math.Abs(float64(best)-months) {
			best = m
		}
	}
	return best, true
}

// nextPayment is day, moved past today by the interval if it lies before.
func nextPayment(day string, months int, today time.Time) string {
	d, ok := ParseDay(day)
	if !ok {
		return ""
	}
	for i := 0; d.Before(today) && i < maxCatchUp; i++ {
		if months == 0 {
			d = d.AddDate(0, 0, daysPerWeek)
			continue
		}
		d = d.AddDate(0, months, 0)
	}
	return d.Format(time.DateOnly)
}

// lastPayment is the newest booking in Sure that pays r.
func lastPayment(bank *sources.BankData, r *sources.BankRecurring) *sources.BankTxn {
	var out *sources.BankTxn
	for i := range bank.Transactions {
		t := &bank.Transactions[i]
		if t.Amount >= 0 || !sameName(r.Name, t.Merchant+" "+t.Name) {
			continue
		}
		if out == nil || t.Date > out.Date {
			out = t
		}
	}
	return out
}

// subVendor is the Ninja vendor named like the subscription.
func subVendor(n *sources.NinjaDataset, name string) *sources.NinjaVendor {
	for i := range n.Vendors {
		if sameName(name, n.Vendors[i].Name) {
			return &n.Vendors[i]
		}
	}
	return nil
}

func vendorKeyOf(v *sources.NinjaVendor) string {
	if v == nil {
		return ""
	}
	return v.Key
}

// ninjaSubName is what a recurring expense is matched by: its vendor,
// else its notes.
func ninjaSubName(vendors map[string]string, e *sources.NinjaRecurringExpense) string {
	if name := vendors[e.VendorKey]; name != "" {
		return name
	}
	return e.Notes
}

func subNinjaName(vendors map[string]string, e *sources.NinjaRecurringExpense) string {
	return strings.TrimSpace(vendors[e.VendorKey] + " " + e.Number)
}

// subID is a case key of an entry with an id: "wp-3", "np-Kx9".
func subID(a SubAction, id string) string { return subPrefix[a] + id }

// subKey is a case key of a name: the action's prefix and the name
// made safe for form fields ("wa-farbraum-grade-abo").
func subKey(a SubAction, id string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(id) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	return subPrefix[a] + strings.TrimSuffix(b.String(), "-")
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func money2(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }
