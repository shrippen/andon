// Package billing turns unbilled Kimai time into Invoice Ninja drafts.
//
//	Candidates: spaces the caller edits with a Kimai and an Invoice Ninja
//	            connection → one draft per customer (stored data, fast)
//	Create:     fresh data → draft invoice in Ninja → optionally flag the
//	            Kimai sheets as exported, so they are not billed twice
package billing

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/outbound"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	auditsvc "andon/internal/services/audit"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/services/verbund"
	"andon/internal/sources"
)

var (
	// ErrNoClient means no Invoice Ninja client carries the customer's name.
	ErrNoClient = errors.New("billing.no_client")
	// ErrNothing means the customer has no unbilled time (any more).
	ErrNothing = errors.New("billing.nothing")
)

// ExportMode says whether billed Kimai sheets get flagged as exported.
type ExportMode bool

const (
	KeepSheets ExportMode = false
	MarkSheets ExportMode = true
)

// Candidate is one customer's draft in one space.
type Candidate struct {
	SpaceID   int64
	SpaceName string
	KimaiID   int64 // the pair's Kimai connection
	metrics.Draft
}

// pair is a space's Kimai and Invoice Ninja connection.
type pair struct {
	space        access.SpaceRef
	kimai, ninja *model.Connection
}

// pairs lists each Kimai of the spaces the caller may EDIT with its
// Invoice Ninja partner (Verbund, or the one there is).
func pairs(d *sql.DB, who *access.Principal, spaceID int64) ([]pair, error) {
	var out []pair
	err := db.WithRead(d, func(tx *sql.Tx) error {
		found, err := verbund.Pairs(tx, who, editable(who, spaceID), enums.ServiceKimai, enums.ServiceInvoiceNinja)
		for _, p := range found {
			if ref, ok := writable(who, p.A, p.B); ok {
				out = append(out, pair{space: ref, kimai: p.A, ninja: p.B})
			}
		}
		return err
	})
	return out, err
}

// editable lists the spaces (or the one asked for) the caller may EDIT.
func editable(who *access.Principal, spaceID int64) []int64 {
	var out []int64
	for id, ref := range who.Spaces {
		if spaceID != 0 && id != spaceID {
			continue
		}
		if access.SpaceRight(who, &ref) >= enums.RightEdit {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// writable is the space of a, when the caller may EDIT the spaces of
// both connections.
func writable(who *access.Principal, a, b *model.Connection) (access.SpaceRef, bool) {
	ref, ok := who.Spaces[a.SpaceID]
	other, ok2 := who.Spaces[b.SpaceID]
	if !ok || !ok2 || access.SpaceRight(who, &other) < enums.RightEdit {
		return access.SpaceRef{}, false
	}
	return ref, true
}

// pick is the pair of the connection connID; 0 takes the space's only
// pair (forms from before Verbünde).
func pick[P any](list []P, connID int64, idOf func(P) int64) (P, bool) {
	var zero P
	if connID == 0 && len(list) == 1 {
		return list[0], true
	}
	for _, p := range list {
		if idOf(p) == connID {
			return p, true
		}
	}
	return zero, false
}

func load(ctx context.Context, d *sql.DB, who *access.Principal, p pair, fresh svcdata.Freshness) (*sources.KimaiDataset, *sources.NinjaDataset, error) {
	uid := who.UserID
	k, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceKimai), nil, p.kimai, model.UserHolder(uid), fresh)
	if err != nil {
		return nil, nil, err
	}
	n, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceInvoiceNinja), nil, p.ninja, model.UserHolder(uid), fresh)
	if err != nil {
		return nil, nil, err
	}
	kimai, _ := k.Data.(*sources.KimaiDataset)
	ninja, _ := n.Data.(*sources.NinjaDataset)
	return kimai, ninja, nil
}

// Candidates lists drafts from the last background run.
func Candidates(ctx context.Context, d *sql.DB, who *access.Principal) ([]Candidate, error) {
	found, err := pairs(d, who, 0)
	if err != nil {
		return nil, err
	}
	var out []Candidate
	for _, p := range found {
		kimai, ninja, err := load(ctx, d, who, p, svcdata.Stored)
		if err != nil || kimai == nil {
			continue
		}
		settings, err := spaceSettings(d, p.space.ID)
		if err != nil {
			return nil, err
		}
		links, err := verbund.ClientMapFor(d, p.kimai.ID, p.ninja.ID)
		if err != nil {
			return nil, err
		}
		for _, draft := range billable(metrics.Drafts(kimai, ninja, links), settings) {
			out = append(out, Candidate{SpaceID: p.space.ID, SpaceName: p.space.Name, KimaiID: p.kimai.ID, Draft: draft})
		}
	}
	return out, nil
}

// billable drops internal work: customers billed at rate 0 and those on
// the space's internal list (settings billing.internal, comma separated).
func billable(drafts []metrics.Draft, settings map[string]any) []metrics.Draft {
	internal := map[string]bool{}
	billing, _ := settings["billing"].(map[string]any)
	list, _ := billing["internal"].(string)
	for _, name := range strings.Split(list, ",") {
		if name = strings.ToLower(strings.TrimSpace(name)); name != "" {
			internal[name] = true
		}
	}
	var out []metrics.Draft
	for _, d := range drafts {
		if d.Total == 0 || internal[strings.ToLower(strings.TrimSpace(d.Customer))] {
			continue
		}
		out = append(out, d)
	}
	return out
}

// spaceSettings reads a space's settings (goals, billing, …).
func spaceSettings(d *sql.DB, spaceID int64) (map[string]any, error) {
	var settings map[string]any
	err := db.WithRead(d, func(tx *sql.Tx) error {
		sp, err := content.Space(tx, spaceID)
		if err != nil || sp == nil {
			return err
		}
		settings = sp.Settings
		return nil
	})
	return settings, err
}

// Create writes one customer's draft to Invoice Ninja and returns its number.
func Create(ctx context.Context, d *sql.DB, who *access.Principal, spaceID, kimaiID, customerID int64, mode ExportMode, ip string) (string, error) {
	found, err := pairs(d, who, spaceID)
	if err != nil {
		return "", err
	}
	p, ok := pick(found, kimaiID, func(p pair) int64 { return p.kimai.ID })
	if !ok {
		return "", access.ErrDenied
	}
	for _, c := range []*model.Connection{p.kimai, p.ninja} {
		if _, err := connections.Get(d, who, c.ID); err != nil {
			return "", err
		}
	}

	// Fresh data: a sheet billed a minute ago must not be billed again.
	kimai, ninja, err := load(ctx, d, who, p, svcdata.Force)
	if err != nil {
		return "", err
	}
	if kimai == nil || ninja == nil {
		return "", ErrNothing
	}
	links, err := verbund.ClientMapFor(d, p.kimai.ID, p.ninja.ID)
	if err != nil {
		return "", err
	}
	draft, ok := draftFor(metrics.Drafts(kimai, ninja, links), customerID)
	if !ok {
		return "", ErrNothing
	}
	if draft.ClientKey == "" {
		return "", ErrNoClient
	}

	ninjaSecret, err := svcdata.Secret(ctx, d, p.ninja, model.UserHolder(who.UserID))
	if err != nil {
		return "", err
	}
	lines := make([]outbound.NinjaLine, 0, len(draft.Lines))
	for _, l := range draft.Lines {
		lines = append(lines, outbound.NinjaLine{Product: l.Product, Notes: l.Notes, Quantity: l.Hours, Cost: l.Rate})
	}
	number, err := outbound.NinjaDraftInvoice(ctx, outbound.Target{URL: p.ninja.URL, Token: ninjaSecret, VerifyTLS: p.ninja.VerifyTLS}, draft.ClientKey, lines)
	if err != nil {
		return "", err
	}

	if mode == MarkSheets {
		kimaiSecret, err := svcdata.Secret(ctx, d, p.kimai, model.UserHolder(who.UserID))
		if err != nil {
			return number, err
		}
		kimai := outbound.Target{URL: p.kimai.URL, Token: kimaiSecret, VerifyTLS: p.kimai.VerifyTLS}
		for _, id := range draft.SheetIDs {
			if err := outbound.KimaiMarkExported(ctx, kimai, id); err != nil {
				return number, err
			}
		}
	}
	svcdata.Forget(p.kimai.ID)
	svcdata.Forget(p.ninja.ID)
	return number, auditsvc.Log(d, &who.UserID, "billing.draft", draft.Customer, ip,
		map[string]any{"number": number, "sheets": len(draft.SheetIDs), "total": strconv.FormatFloat(draft.Total, 'f', 2, 64)})
}

func draftFor(drafts []metrics.Draft, customerID int64) (metrics.Draft, bool) {
	for _, d := range drafts {
		if d.CustomerID == customerID {
			return d, true
		}
	}
	return metrics.Draft{}, false
}

// Figures is what the period brought across the caller's pairs, for the
// billing page; HoursFrom and RevenueFrom are where Kimai's and Invoice
// Ninja's data begin (the latest of the pairs; zero = unknown).
type Figures struct {
	Span                   metrics.Span
	Currency               string
	HoursFrom, RevenueFrom time.Time
	metrics.PeriodSums
}

// FiguresOf sums the period over the stored data of every pair.
func FiguresOf(ctx context.Context, d *sql.DB, who *access.Principal, period metrics.Period, today time.Time) (Figures, error) {
	out := Figures{Span: period.Span(today)}
	found, err := pairs(d, who, 0)
	if err != nil {
		return out, err
	}
	for _, p := range found {
		kimai, ninja, err := load(ctx, d, who, p, svcdata.Stored)
		if err != nil {
			continue
		}
		sums := metrics.PeriodSumsOf(kimai, ninja, out.Span)
		out.Revenue += sums.Revenue
		out.Paid += sums.Paid
		out.Expenses += sums.Expenses
		out.Hours += sums.Hours
		if kimai != nil {
			out.HoursFrom = later(out.HoursFrom, metrics.KimaiHistoryFrom(kimai))
		}
		if ninja != nil {
			out.RevenueFrom = later(out.RevenueFrom, metrics.NinjaHistoryFrom(ninja))
			out.Currency = cmp.Or(out.Currency, ninja.Currency)
		}
	}
	return out, nil
}

// later is the later of two days.
func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}
