package verbund

// Categories of a Verbund, both kept on its Invoice Ninja (options
// sources.OptCategoryMap, OptEuerSlot, OptEuerMap):
//
//	Sure category ──► Ninja expense category   (Sure's auto-tags help /vat)
//	Ninja category ─► EÜR line, an expense custom field (the EÜR bench)
//
// Andon proposes from bookings both sides have (metrics.SureCategoryVotes,
// EuerVotes); nothing counts until the user saves.

import (
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
	Sure       []SureCategory
	Fields     [sources.NinjaSlots]sources.ExpenseField
	Slot       int // the EÜR field (1–4), 0 none
	Euer       []EuerRow
	CanSave    bool // MANAGE on the Invoice Ninja
}

// SureCategory is one category of Sure with its Ninja category.
type SureCategory struct {
	Name      string
	Count     int    // its debits sharing a booking with an expense
	Mapped    string // saved Ninja key, "" none
	Suggested string // Ninja key the bookings say, "" none
}

// EuerRow is one Ninja category with its EÜR line.
type EuerRow struct {
	Key, Name string
	Count     int    // its expenses with that line
	Mapped    string // saved line
	Suggested string
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

// Lines are the EÜR field's choices: a dropdown's, else every value its
// expenses carry.
func (v CategoryView) Lines() []string {
	if v.Slot == 0 {
		return nil
	}
	if opts := v.Fields[v.Slot-1].Options; len(opts) > 0 {
		return opts
	}
	var out []string
	for _, r := range v.Euer {
		for _, s := range []string{r.Mapped, r.Suggested} {
			if s != "" && !slices.Contains(out, s) {
				out = append(out, s)
			}
		}
	}
	sort.Strings(out)
	return out
}

// HasNinja reports whether the Verbund has an Invoice Ninja (its
// categories page).
func (v View) HasNinja() bool { _, ok := v.Has(enums.ServiceInvoiceNinja); return ok }

// CategoriesOf lists a Verbund's categories. Requires VIEW on every member.
func CategoriesOf(ctx context.Context, d *sql.DB, who *access.Principal, id int64) (CategoryView, error) {
	v, err := Get(d, who, id)
	if err != nil {
		return CategoryView{}, err
	}
	out := CategoryView{Verbund: v}
	m, ok := v.Has(enums.ServiceInvoiceNinja)
	if !ok {
		return out, ErrNoNinja
	}
	if out.Ninja, err = connections.ByID(d, m.ConnID); err != nil || out.Ninja == nil {
		return out, errors.Join(err, ErrNoNinja)
	}
	for _, s := range []enums.ServiceType{enums.ServiceSure, enums.ServiceFirefly} {
		if bm, ok := v.Has(s); ok && out.Bank == nil {
			if out.Bank, err = connections.ByID(d, bm.ConnID); err != nil {
				return out, err
			}
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
	var bank *sources.SureDataset
	if len(data) > 1 {
		bank, _ = data[1].(*sources.SureDataset)
	}
	if cv, err := connections.Get(d, who, out.Ninja.ID); err == nil {
		out.CanSave = cv.Right >= enums.RightManage
	}

	saved := sources.CategoriesOf(out.Ninja.Options)
	out.Categories, out.Fields = ninja.Categories, ninja.ExpenseFields
	sort.SliceStable(out.Categories, func(a, b int) bool {
		return strings.ToLower(out.Categories[a].Name) < strings.ToLower(out.Categories[b].Name)
	})
	if bank != nil {
		out.Sure = sureCategories(ninja, bank, saved.Sure[out.Bank.ID])
	}
	out.Slot = saved.EuerSlot
	if out.Slot == 0 {
		out.Slot = euerSlot(out.Fields)
	}
	out.Euer = euerRows(ninja, out.Categories, out.Slot, saved.Euer)
	return out, nil
}

// sureCategories lists Sure's categories of debits, the mapped and the
// suggested Ninja category: by shared bookings, else the same name.
func sureCategories(ninja *sources.NinjaDataset, bank *sources.SureDataset, saved map[string]string) []SureCategory {
	votes := metrics.SureCategoryVotes(ninja, bank)
	var names []string
	for _, t := range bank.Transactions {
		if t.Category != "" && t.Amount < 0 && !slices.Contains(names, t.Category) {
			names = append(names, t.Category)
		}
	}
	sort.Strings(names)
	out := make([]SureCategory, 0, len(names))
	for _, n := range names {
		c := SureCategory{Name: n, Mapped: saved[n]}
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

func euerRows(ninja *sources.NinjaDataset, cats []sources.NinjaCategory, slot int, saved map[string]string) []EuerRow {
	votes := metrics.EuerVotes(ninja, slot)
	out := make([]EuerRow, 0, len(cats))
	for _, c := range cats {
		r := EuerRow{Key: c.Key, Name: c.Name, Mapped: saved[c.Key]}
		r.Suggested, r.Count = votes.Best(c.Key)
		out = append(out, r)
	}
	return out
}

// CategorySave is what the page sends: Sure's categories, the EÜR field
// and each Ninja category's line ("" removes one).
type CategorySave struct {
	Sure map[string]string
	Slot int
	Euer map[string]string
}

// SaveCategories stores them on the Verbund's Invoice Ninja. Requires
// MANAGE on it.
func SaveCategories(ctx context.Context, d *sql.DB, who *access.Principal, id int64, in CategorySave, ip string) error {
	v, err := CategoriesOf(ctx, d, who, id)
	if err != nil {
		return err
	}
	keys := map[string]bool{}
	for _, c := range v.Categories {
		keys[c.Key] = true
	}
	c := sources.CategoriesOf(v.Ninja.Options)
	if v.Bank != nil {
		c.Sure[v.Bank.ID] = clean(in.Sure, func(_, val string) bool { return keys[val] })
	}
	if in.Slot >= 0 && in.Slot <= sources.NinjaSlots {
		c.EuerSlot = in.Slot
	}
	c.Euer = clean(in.Euer, func(key, _ string) bool { return keys[key] })

	options := maps.Clone(v.Ninja.Options)
	if options == nil {
		options = map[string]any{}
	}
	c.Options(options)
	if err := connections.SetOptions(d, who, v.Ninja.ID, options); err != nil {
		return err
	}
	return audit.Log(d, &who.UserID, "verbund.categories", v.Verbund.Name, ip,
		map[string]any{"sure": len(c.Sure[bankID(v.Bank)]), "euer": len(c.Euer), "slot": c.EuerSlot})
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
