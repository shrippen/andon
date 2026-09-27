package receipts

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/services/access"
	"andon/internal/services/connections"
	"andon/internal/sources"
)

// Slot is one Invoice Ninja expense custom value, e.g. 2 "Paperless".
type Slot struct {
	N     int
	Label string // "" = unnamed
}

// FieldSetup is the mapping form: what exists on both sides, what is
// chosen (or suggested by name while nothing is), and who may change it.
type FieldSetup struct {
	Mapping   Mapping
	Suggested bool // Mapping is a guess from the field names
	Slots     []Slot
	Fields    []sources.DocField
	CanEdit   bool // manage right on both connections
	NinjaName string
	DocsName  string
}

// Fields reads the custom fields of both sides for the mapping form.
func Fields(ctx context.Context, d *sql.DB, who *access.Principal) (FieldSetup, error) {
	p, err := openPair(d, who)
	if err != nil {
		return FieldSetup{}, err
	}
	out := FieldSetup{Mapping: p.mapping, NinjaName: p.ninja.Name, DocsName: p.docs.Name, CanEdit: canManage(d, who, p.ninja) && canManage(d, who, p.docs)}
	expenses, err := p.expenses(ctx, d, who)
	if err != nil {
		return out, err
	}
	for i, label := range expenses.Slots {
		out.Slots = append(out.Slots, Slot{N: i + 1, Label: label})
	}
	docs, err := p.docSet(ctx, d, who, time.Now().Year())
	if err != nil {
		return out, err
	}
	out.Fields = docs.Fields
	if out.Mapping == (Mapping{}) {
		out.Mapping, out.Suggested = suggest(out.Slots, out.Fields), true
	}
	return out, nil
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
