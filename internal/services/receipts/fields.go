package receipts

import (
	"cmp"
	"context"
	"database/sql"
	"slices"
	"strings"
	"time"

	"andon/internal/repos/users"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/sources"
)

// Slot is one Invoice Ninja expense custom value, e.g. 2 "Paperless",
// and how many expenses fill it.
type Slot struct {
	N     int
	Label string // "" = unnamed
	Used  int
}

// Warning is a doubt about the mapping, as i18n key and params.
type Warning struct {
	Key  string
	Args map[string]any
}

// FieldSetup is the mapping form: what exists on both sides, what is
// chosen (or suggested by name while nothing is), who may change it and
// who else could.
type FieldSetup struct {
	Mapping   Mapping
	Suggested bool // Mapping is a guess from the field names
	Slots     []Slot
	Fields    []sources.DocField
	Tags      []string
	Warnings  []Warning
	CanEdit   bool     // manage right on both connections
	Managers  []string // who else may, when the user may not
}

// SlotName is a slot's label for the read-only view.
func (f FieldSetup) SlotName(n int) string {
	for _, s := range f.Slots {
		if s.N == n {
			return s.Label
		}
	}
	return ""
}

// FieldName is a Paperless field's name for the read-only view.
func (f FieldSetup) FieldName(id int64) string {
	for _, x := range f.Fields {
		if x.ID == id {
			return x.Name
		}
	}
	return ""
}

// Fields reads the custom fields of both sides for the mapping form.
func Fields(ctx context.Context, d *sql.DB, who *access.Principal) (FieldSetup, error) {
	p, err := openPair(d, who)
	if err != nil {
		return FieldSetup{}, err
	}
	out := FieldSetup{Mapping: p.mapping, CanEdit: canManage(d, who, p.ninja) && canManage(d, who, p.docs)}
	expenses, err := p.expenses(ctx, d, who)
	if err != nil {
		return out, err
	}
	for i, label := range expenses.Slots {
		slot := Slot{N: i + 1, Label: label}
		for _, e := range expenses.Expenses {
			if e.Custom[i] != "" {
				slot.Used++
			}
		}
		out.Slots = append(out.Slots, slot)
	}
	docs, err := p.docSet(ctx, d, who, time.Now().Year())
	if err != nil {
		return out, err
	}
	out.Fields, out.Tags = docs.Fields, docs.TagNames
	if out.Mapping == (Mapping{}) {
		out.Mapping, out.Suggested = suggest(out.Slots, out.Fields), true
	}
	out.Warnings = warnings(out.Mapping, out.Fields)
	if !out.CanEdit {
		out.Managers = managers(d, who, p)
	}
	return out, nil
}

// Field types each part expects.
var (
	textTypes   = []string{"string", "longtext"}
	amountTypes = []string{"monetary", "float", "string"}
)

// warnings lists chosen fields whose type does not fit their part.
func warnings(m Mapping, fields []sources.DocField) []Warning {
	var out []Warning
	check := func(id int64, want []string, key string) {
		for _, f := range fields {
			if f.ID == id && id > 0 && !slices.Contains(want, f.Type) {
				out = append(out, Warning{Key: key, Args: map[string]any{"field": f.Name}})
			}
		}
	}
	check(m.FieldInvoice, textTypes, "receipts.warn_text_type")
	check(m.FieldExpense, textTypes, "receipts.warn_text_type")
	check(m.FieldLink, []string{"url"}, "receipts.warn_link_type")
	check(m.FieldAmount, amountTypes, "receipts.warn_amount_type")
	return out
}

// twice tells whether one slot or field serves two parts of the link.
func (m Mapping) twice() bool {
	if m.InvoiceSlot > 0 && m.InvoiceSlot == m.LinkSlot {
		return true
	}
	seen := map[int64]bool{}
	for _, id := range []int64{m.FieldInvoice, m.FieldExpense, m.FieldLink, m.FieldAmount} {
		if id > 0 && seen[id] {
			return true
		}
		seen[id] = true
	}
	return false
}

// managers names the other active users who manage both connections.
func managers(d *sql.DB, who *access.Principal, p pair) []string {
	all, err := users.All(d)
	if err != nil {
		return nil
	}
	var out []string
	for _, u := range all {
		if !u.IsActive || u.ID == who.UserID {
			continue
		}
		them, err := access.Load(d, u.ID)
		if err != nil || them == nil || !canManage(d, them, p.ninja) || !canManage(d, them, p.docs) {
			continue
		}
		out = append(out, cmp.Or(u.Name, u.Email))
	}
	return out
}

func canManage(d *sql.DB, who *access.Principal, conn *model.Connection) bool {
	v, err := connections.Get(d, who, conn.ID)
	return err == nil && v.Right >= enums.RightManage
}

// Name hints of the suggestion, PaperNinja's.
var (
	hintInvoice = []string{"rechnungsnummer", "invoice number", "invoice no", "rechnung", "invoice"}
	hintLink    = []string{"paperless", "beleg", "receipt", "link", "url"}
	hintExpense = []string{"ausgabe", "expense"}
	hintNinja   = []string{"invoice ninja", "invoiceninja", "ninja", "link"}
	hintAmount  = []string{"betrag", "amount", "summe", "total"}
)

func hinted(name string, hints []string) bool {
	name = strings.ToLower(name)
	for _, h := range hints {
		if strings.Contains(name, h) {
			return true
		}
	}
	return false
}

// suggest guesses the mapping from the field names and types.
func suggest(slots []Slot, fields []sources.DocField) Mapping {
	var m Mapping
	for _, s := range slots {
		switch {
		case m.InvoiceSlot == 0 && hinted(s.Label, hintInvoice):
			m.InvoiceSlot = s.N
		case m.LinkSlot == 0 && hinted(s.Label, hintLink):
			m.LinkSlot = s.N
		}
	}
	for _, f := range fields {
		switch {
		case m.FieldExpense == 0 && hinted(f.Name, hintExpense):
			m.FieldExpense = f.ID
		case m.FieldLink == 0 && (f.Type == "url" || hinted(f.Name, hintNinja)):
			m.FieldLink = f.ID
		case m.FieldAmount == 0 && (f.Type == "monetary" || hinted(f.Name, hintAmount)):
			m.FieldAmount = f.ID
		case m.FieldInvoice == 0 && hinted(f.Name, hintInvoice):
			m.FieldInvoice = f.ID
		}
	}
	return m
}

// SaveFields stores the mapping on both connections. It needs the manage
// right on both, since everybody using them shares the mapping.
func SaveFields(d *sql.DB, who *access.Principal, m Mapping) error {
	p, err := openPair(d, who)
	if err != nil {
		return err
	}
	if !canManage(d, who, p.ninja) || !canManage(d, who, p.docs) {
		return ErrManage
	}
	if m.twice() {
		return ErrFieldTwice
	}
	ninja := withOptions(p.ninja.Options, map[string]any{optInvoiceSlot: m.InvoiceSlot, optLinkSlot: m.LinkSlot})
	docs := withOptions(p.docs.Options, map[string]any{optFieldInvoice: m.FieldInvoice, optFieldExpense: m.FieldExpense,
		optFieldLink: m.FieldLink, optFieldAmount: m.FieldAmount, optQueueTag: strings.TrimSpace(m.QueueTag)})
	if err := connections.SetOptions(d, who, p.ninja.ID, ninja); err != nil {
		return err
	}
	return connections.SetOptions(d, who, p.docs.ID, docs)
}

// withOptions copies options and sets the given ones; zero values and
// empty strings remove an option.
func withOptions(options, set map[string]any) map[string]any {
	out := make(map[string]any, len(options)+len(set))
	for k, v := range options {
		out[k] = v
	}
	for k, v := range set {
		switch t := v.(type) {
		case int:
			if t == 0 {
				delete(out, k)
				continue
			}
		case int64:
			if t == 0 {
				delete(out, k)
				continue
			}
		case string:
			if t == "" {
				delete(out, k)
				continue
			}
		}
		out[k] = v
	}
	return out
}
