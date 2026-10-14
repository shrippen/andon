package receipts

// Check lets Claude read the scans a review would link and compares
// them with their expense, on request only (the files leave Andon):
//
//	scan ──download──► Claude: gross, VAT, date ──► against the expense:
//	                                                amount, input VAT
//
// Readings are kept per scan, so a second review asks nothing again.

import (
	"context"
	"database/sql"
	"math"
	"strconv"

	"andon/internal/outbound"
	"andon/internal/services/access"
	"andon/internal/services/assist"
	"andon/internal/sources"
)

// ScanCheck is what Claude read from a step's scans and what differs.
type ScanCheck struct {
	Read  assist.Invoice // the scans together: gross and VAT summed
	Notes []Note         // empty: amount and tax fit
	Error string         // i18n key or message when reading failed
}

// Note is one difference: an i18n key and its parameters.
type Note struct {
	Key  string
	Args map[string]any
}

// taxTol is how far the input VAT may differ (rounding per line).
const taxTol = 0.05

// Check reads the scans of every sound link and new-expense step.
func Check(ctx context.Context, d *sql.DB, who *access.Principal, plan *HuntPlan) error {
	if !assist.Enabled() {
		return assist.ErrOff
	}
	p, err := openPair(d, who)
	if err != nil {
		return err
	}
	sctx, err := p.sourceCtx(d, who, p.docs)
	if err != nil {
		return err
	}
	reads := map[string]assist.Invoice{}
	for i := range plan.Steps {
		st := &plan.Steps[i]
		if st.Problem != "" || len(st.Item.Docs) == 0 {
			continue
		}
		c := &ScanCheck{}
		for _, doc := range st.Item.Docs {
			key := docKey(p.docs.ID, doc.ID)
			inv, ok := p.state.Reads[key]
			if !ok {
				if inv, err = readScan(ctx, sctx, doc.ID); err != nil {
					c.Error = err.Error()
					break
				}
				reads[key] = inv
			}
			c.Read.Gross += inv.Gross
			c.Read.VAT += inv.VAT
			c.Read.Vendor, c.Read.Number, c.Read.Date = inv.Vendor, inv.Number, inv.Date
		}
		if c.Error == "" {
			c.Notes = compare(st.Item.Expense, c.Read)
		}
		st.Item.Check = c
	}
	if len(reads) == 0 {
		return nil
	}
	return saveState(d, who, func(s *state) {
		for k, v := range reads {
			s.Reads[k] = v
		}
	})
}

func docKey(conn, id int64) string {
	return "p" + strconv.FormatInt(conn, 10) + "/" + strconv.FormatInt(id, 10)
}

// readScan downloads one scan and lets Claude read it.
func readScan(ctx context.Context, sctx sources.Ctx, id int64) (assist.Invoice, error) {
	body, kind, err := sources.PaperlessFile(ctx, sctx, id)
	if err != nil {
		return assist.Invoice{}, err
	}
	return assist.ReadInvoice(ctx, []outbound.LLMFile{{Media: kind, Content: body}})
}

// compare names what the scans say differently from the expense: the
// gross amount, and the input VAT the expense books (inclusive taxes).
func compare(e sources.ReceiptExpense, read assist.Invoice) []Note {
	var out []Note
	if read.Gross > 0 && math.Abs(read.Gross-e.Amount) > amountTol {
		out = append(out, Note{Key: "receipts.check_amount", Args: map[string]any{"scan": money(read.Gross), "expense": money(e.Amount)}})
	}
	booked := 0.0
	if e.TaxRate > 0 {
		booked = e.Amount * e.TaxRate / (100 + e.TaxRate)
	}
	switch {
	case read.VAT > 0 && booked == 0:
		out = append(out, Note{Key: "receipts.check_vat_missing", Args: map[string]any{"vat": money(read.VAT)}})
	case read.VAT == 0 && booked > 0:
		out = append(out, Note{Key: "receipts.check_vat_none", Args: map[string]any{"vat": money(booked)}})
	case math.Abs(read.VAT-booked) > taxTol:
		out = append(out, Note{Key: "receipts.check_vat", Args: map[string]any{"scan": money(read.VAT), "expense": money(booked)}})
	}
	return out
}
