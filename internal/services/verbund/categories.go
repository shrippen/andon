package verbund

// Categories of a Verbund, kept on its Invoice Ninja (options
// sources.OptCategoryMap, OptTagMap, OptEuerSlot):
//
//	Sure category ──► Ninja expense category   (Sure's auto-tags help /vat)
//	Sure tag ───────► Ninja expense category   (when the category has none)
//	EÜR field ──────► the expense custom field holding the EÜR line
//
// The EÜR line belongs to each expense, not its category (the EÜR
// bench proposes it from the vendor). Andon proposes Sure's mapping from
// bookings both sides have (metrics.SureCategoryVotes); nothing counts
// until the user saves.

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"maps"
	"slices"
	"sort"
	"strings"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/services/access"
	"andon/internal/services/audit"
	"andon/internal/services/connections"
	"andon/internal/sources"
)

// ErrNoNinja: the Verbund has no Invoice Ninja to keep categories on.
var ErrNoNinja = errors.New("verbund.no_ninja")

// euerWords name a custom field that holds the EÜR line.
var euerWords = []string{"eür", "euer", "anlage"}

// CategoryView is the categories page of one Verbund.
type CategoryView struct {
	Verbund    View
	Ninja      *model.Connection
	Bank       *model.Connection // Sure or Firefly, nil if none
	Categories []sources.NinjaCategory
	Sure       []BankCategory
	Tags       []BankCategory // Sure's tags, listed like Sure
	Fields     [sources.NinjaSlots]sources.ExpenseField
	Slot       int // the EÜR field (1–4), 0 none
	lines      []string
	CanSave    bool // MANAGE on the Invoice Ninja
}

// BankCategory is one category (or tag) of Sure with its Ninja category.
type BankCategory struct {
	Name      string
	Count     int    // its debits sharing a booking with an expense
	Mapped    string // saved Ninja key, "" none
	Suggested string // Ninja key the bookings say, "" none
}

// Slot is a named expense custom field and its number (1–4).
type Slot struct {
	N     int
	Label string
}

// Slots lists the named custom fields, to pick the EÜR one from.
func (v CategoryView) Slots() []Slot {
	var out []Slot
	for i, f := range v.Fields {
		if f.Label != "" {
			out = append(out, Slot{N: i + 1, Label: f.Label})
		}
	}
	return out
}

// Lines are the EÜR field's choices (metrics.EuerChoices).
func (v CategoryView) Lines() []string { return v.lines }

// HasNinja reports whether the Verbund has an Invoice Ninja (its
// categories page).
func (v View) HasNinja() bool { _, ok := v.Has(enums.ServiceInvoiceNinja); return ok }

// CategoriesOf lists a Verbund's categories. Requires VIEW on every member.
func CategoriesOf(ctx context.Context, d *sql.DB, who *access.Principal, id int64) (CategoryView, error) {
	v, err := Get(d, who, id)
	if err != nil {
		return CategoryView{}, err
	}
	m, ok := v.Has(enums.ServiceInvoiceNinja)
	if !ok {
		return CategoryView{Verbund: v}, ErrNoNinja
	}
	var bank int64
	for _, s := range []enums.ServiceType{enums.ServiceSure, enums.ServiceFirefly} {
		if bm, ok := v.Has(s); ok && bank == 0 {
			bank = bm.ConnID
		}
	}
	out, err := CategoriesFor(ctx, d, who, m.ConnID, bank)
	out.Verbund = v
	return out, err
}

// CategoriesFor lists the categories of one Invoice Ninja and its Sure
// or Firefly (0: none), for a workbench outside a stored Verbund.
// Requires VIEW on both.
func CategoriesFor(ctx context.Context, d *sql.DB, who *access.Principal, ninjaID, bankID int64) (CategoryView, error) {
	var out CategoryView
	nv, err := connections.Get(d, who, ninjaID)
	if err != nil || nv.Service != enums.ServiceInvoiceNinja {
		return out, errors.Join(err, ErrNoNinja)
	}
	out.CanSave = nv.Right >= enums.RightManage
	if out.Ninja, err = connections.ByID(d, ninjaID); err != nil || out.Ninja == nil {
		return out, errors.Join(err, ErrNoNinja)
	}
	if bankID != 0 {
		if _, err := connections.Get(d, who, bankID); err != nil {
			return out, err
		}
		if out.Bank, err = connections.ByID(d, bankID); err != nil {
			return out, err
		}
	}
	conns := []*model.Connection{out.Ninja}
	if out.Bank != nil {
		conns = append(conns, out.Bank)
	}
	data, err := accountData(ctx, d, who, conns)
	if err != nil {
		return out, err
	}
	ninja, _ := data[0].(*sources.NinjaDataset)
	if ninja == nil {
		return out, ErrNoData
	}
	var bank *sources.BankData
	if len(data) > 1 {
		bank, _ = data[1].(*sources.BankData)
	}

	saved := sources.CategoriesOf(out.Ninja.Options)
	out.Categories, out.Fields = ninja.Categories, ninja.ExpenseFields
	sort.SliceStable(out.Categories, func(a, b int) bool {
		return strings.ToLower(out.Categories[a].Name) < strings.ToLower(out.Categories[b].Name)
	})
	if bank != nil {
		out.Sure = bankNames(ninja, metrics.SureCategoryVotes(ninja, bank), debitNames(bank, catNames), saved.Sure[out.Bank.ID])
		out.Tags = bankNames(ninja, metrics.SureTagVotes(ninja, bank), debitNames(bank, tagNames), saved.Tags[out.Bank.ID])
	}
	out.Slot = saved.EuerSlot
	if out.Slot == 0 {
		out.Slot = euerSlot(out.Fields)
	}
	out.lines = metrics.EuerChoices(ninja, out.Slot)
	return out, nil
}

// catNames and tagNames are what a booking is named by in Sure.
func catNames(t sources.BankTxn) []string { return []string{t.Category} }
func tagNames(t sources.BankTxn) []string { return t.Tags }

// debitNames are the sorted names of Sure's debits.
func debitNames(bank *sources.BankData, of func(sources.BankTxn) []string) []string {
	var names []string
	for _, t := range bank.Transactions {
		if t.Amount >= 0 {
			continue
		}
		for _, n := range of(t) {
			if n != "" && !slices.Contains(names, n) {
				names = append(names, n)
			}
		}
	}
	sort.Strings(names)
	return names
}

// bankNames lists Sure's categories (or tags) of debits, the mapped and
// the suggested Ninja category: by shared bookings, else the same name.
func bankNames(ninja *sources.NinjaDataset, votes metrics.Votes, names []string, saved map[string]string) []BankCategory {
	out := make([]BankCategory, 0, len(names))
	for _, n := range names {
		c := BankCategory{Name: n, Mapped: saved[n]}
		c.Suggested, c.Count = votes.Best(n)
		if c.Suggested == "" {
			for _, nc := range ninja.Categories {
				if strings.EqualFold(nc.Name, n) {
					c.Suggested = nc.Key
				}
			}
		}
		out = append(out, c)
	}
	return out
}

// euerSlot is the field whose name says EÜR, 0 if none.
func euerSlot(fields [sources.NinjaSlots]sources.ExpenseField) int {
	for i, f := range fields {
		label := strings.ToLower(f.Label)
		if slices.ContainsFunc(euerWords, func(w string) bool { return strings.Contains(label, w) }) {
			return i + 1
		}
	}
	return 0
}

// CategorySave is what the page sends: Sure's categories and the EÜR
// field.
type CategorySave struct {
	Sure, Tags map[string]string
	Slot       int
}

// SaveCategories stores them on the Verbund's Invoice Ninja. Requires
// MANAGE on it.
func SaveCategories(ctx context.Context, d *sql.DB, who *access.Principal, id int64, in CategorySave, ip string) error {
	v, err := CategoriesOf(ctx, d, who, id)
	if err != nil {
		return err
	}
	return v.save(d, who, in, ip)
}

// SaveCategoriesFor stores them on an Invoice Ninja outside a Verbund.
func SaveCategoriesFor(ctx context.Context, d *sql.DB, who *access.Principal, ninjaID, bankID int64, in CategorySave, ip string) error {
	v, err := CategoriesFor(ctx, d, who, ninjaID, bankID)
	if err != nil {
		return err
	}
	return v.save(d, who, in, ip)
}

func (v CategoryView) save(d *sql.DB, who *access.Principal, in CategorySave, ip string) error {
	keys := map[string]bool{}
	for _, c := range v.Categories {
		keys[c.Key] = true
	}
	c := sources.CategoriesOf(v.Ninja.Options)
	if v.Bank != nil {
		known := func(_, val string) bool { return keys[val] }
		c.Sure[v.Bank.ID], c.Tags[v.Bank.ID] = clean(in.Sure, known), clean(in.Tags, known)
	}
	if in.Slot >= 0 && in.Slot <= sources.NinjaSlots {
		c.EuerSlot = in.Slot
	}

	options := maps.Clone(v.Ninja.Options)
	if options == nil {
		options = map[string]any{}
	}
	c.Options(options)
	if err := connections.SetOptions(d, who, v.Ninja.ID, options); err != nil {
		return err
	}
	return audit.Log(d, &who.UserID, "verbund.categories", cmp.Or(v.Verbund.Name, v.Ninja.Name), ip,
		map[string]any{"sure": len(c.Sure[bankID(v.Bank)]), "tags": len(c.Tags[bankID(v.Bank)]), "slot": c.EuerSlot})
}

// clean keeps the filled pairs keep accepts.
func clean(in map[string]string, keep func(key, val string) bool) map[string]string {
	out := map[string]string{}
	for k, val := range in {
		if val = strings.TrimSpace(val); val != "" && keep(k, val) {
			out[k] = val
		}
	}
	return out
}

func bankID(c *model.Connection) int64 {
	if c == nil {
		return 0
	}
	return c.ID
}
