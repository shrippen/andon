// Package vatcheck prepares the VAT return from Invoice Ninja's bank
// feed, with Sure beside it (matching: metrics/vatcheck.go):
//
//	Open    the period's open transactions with proposals, and the check
//	Review  the steps the user ticked, as they would be written
//	Apply   writes them, only after the user confirmed the review
//	Skip    sets a transaction aside (private) or back, in Andon only
//	Guess   asks the language model about the open ones, on request
//
//	  Invoice Ninja ──bank feed──┐
//	  Sure ──bookings, tags──────┼─► cases ─► review ─► confirm ─► Ninja
//	  Claude (on request) ───────┘
package vatcheck

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/services/access"
	"andon/internal/services/assist"
	"andon/internal/services/connections"
	"andon/internal/services/svcdata"
	"andon/internal/services/verbund"
	"andon/internal/sources"
)

var (
	// ErrNoNinja means the user reaches no Invoice Ninja.
	ErrNoNinja = errors.New("vatcheck.err_no_ninja")
	// ErrNoBank means Invoice Ninja's bank feed did not answer.
	ErrNoBank = errors.New("vatcheck.err_no_bank")
	// ErrDemo means a demo connection, which takes no writes.
	ErrDemo = errors.New("vatcheck.err_demo")
	// ErrNothing means no step was ticked.
	ErrNothing = errors.New("vatcheck.err_nothing")
)

// spansShown is how many periods the page offers, this one included.
const spansShown = 5

// Choice is one Invoice Ninja the user can check.
type Choice struct {
	ID   int64
	Name string
}

// Page is the VAT check of one Invoice Ninja and period.
type Page struct {
	Choices  []Choice
	Conn     int64
	Bank     string // the Sure or Firefly beside it, "" none
	Spans    []metrics.VATSpan
	Span     metrics.VATSpan
	Cases    []Case
	Skipped  []sources.NinjaBankTxn
	Check    metrics.VATCheck
	Ledger   Ledger
	Interval string // monthly, quarterly
	Method   string // ist, soll
	Currency string
	URL      string // Invoice Ninja, for links
	Assist   bool   // the language model is set up
	Guessed  bool   // it answered for this period
}

// Case is one open transaction with the model's reading, if asked.
type Case struct {
	metrics.BankCase
	Guess *assist.Guess
}

// Ledger is what a new expense can choose from.
type Ledger struct {
	Vendors    []string
	Categories []sources.NinjaCategory
	TaxRates   []sources.NinjaTaxRate
}

// book is the loaded data of one check.
type book struct {
	conn     *model.Connection
	bankConn *model.Connection
	ninja    *sources.NinjaDataset
	bank     *sources.SureDataset
	settings map[string]any
	state    state
}

// Open builds the page for connection connID (0: the last or only one)
// and period key ("" = the last finished period).
func Open(ctx context.Context, d *sql.DB, who *access.Principal, connID int64, spanKey string, today time.Time) (Page, error) {
	var p Page
	b, choices, err := load(ctx, d, who, connID, svcdata.Stored)
	p.Choices = choices
	if err != nil {
		return p, err
	}
	p.Conn, p.URL, p.Currency, p.Assist = b.conn.ID, b.conn.URL, b.ninja.Currency, assist.Enabled()
	if b.bankConn != nil {
		p.Bank = b.bankConn.Name
	}
	p.Interval, p.Method = metrics.TaxVATInterval(b.settings), metrics.TaxVATMethod(b.settings)
	p.Spans = metrics.VATSpans(today, p.Interval, spansShown)
	p.Span = defaultSpan(p.Spans, b.settings, today)
	if s, ok := metrics.VATSpanOf(spanKey, p.Interval); ok {
		p.Span = s
	}
	p.Ledger = Ledger{Categories: b.ninja.Categories, TaxRates: b.ninja.TaxRates}
	for _, v := range b.ninja.Vendors {
		p.Ledger.Vendors = append(p.Ledger.Vendors, v.Name)
	}
	if !b.ninja.BankRead {
		return p, ErrNoBank
	}

	in := b.inputs(p.Span, p.Method, today)
	cases := metrics.BankCases(in)
	p.Check = metrics.VATCheckOf(in, cases)
	guesses := b.state.guesses(b.conn.ID)
	for _, c := range cases {
		g, ok := guesses[c.Txn.Key]
		cs := Case{BankCase: c}
		if ok {
			cs.Guess = &g
			p.Guessed = true
			b.apply(&cs)
		}
		p.Cases = append(p.Cases, cs)
	}
	for _, t := range b.ninja.BankTxns {
		if in.Skip[t.Key] && inSpan(t.Date, p.Span) {
			p.Skipped = append(p.Skipped, t)
		}
	}
	return p, nil
}

// returnDays is when a return is due after its period ends, without
// tax settings: the 10th of the next month.
const returnDays = 10

// defaultSpan is the last finished period until its return is due, then
// the running one: September's return is worked on until 10 October.
func defaultSpan(spans []metrics.VATSpan, settings map[string]any, today time.Time) metrics.VATSpan {
	last := spans[1]
	due := last.End.AddDate(0, 0, returnDays)
	if tax, ok := metrics.ParseTaxSettings(settings); ok {
		if d, found := metrics.VATReturnFor(tax, last.End); found {
			due = d.Due
		}
	}
	if today.After(due) {
		return spans[0]
	}
	return last
}

// apply lets the model's reading fill what the data left open; it never
// replaces a vendor's own history.
func (b book) apply(c *Case) {
	g := c.Guess
	if g.Private && c.Action != metrics.ActionLink {
		c.Action = metrics.ActionSkip
		return
	}
	if !c.Txn.Debit || c.Draft.From == metrics.FromHistory {
		return
	}
	if c.Draft.VendorKey == "" && g.Vendor != "" {
		c.Draft.Vendor = g.Vendor
		for _, v := range b.ninja.Vendors {
			if equalFold(v.Name, g.Vendor) {
				c.Draft.VendorKey = v.Key
			}
		}
	}
	if c.Draft.CategoryKey == "" {
		for _, cat := range b.ninja.Categories {
			if equalFold(cat.Name, g.Category) {
				c.Draft.Category, c.Draft.CategoryKey = cat.Name, cat.Key
			}
		}
	}
	if !c.Draft.TaxKnown && g.TaxKnown {
		c.Draft.TaxRate, c.Draft.TaxKnown = g.TaxRate, true
	}
	c.Draft.From = metrics.FromModel
}

func inSpan(day string, s metrics.VATSpan) bool {
	d, ok := metrics.ParseDay(day)
	return ok && !d.Before(s.Start) && !d.After(s.End)
}

func (b book) inputs(s metrics.VATSpan, method string, today time.Time) metrics.VATInputs {
	skip := map[string]bool{}
	for _, k := range b.state.skipped(b.conn.ID) {
		skip[k] = true
	}
	return metrics.VATInputs{Ninja: b.ninja, Sure: b.bank, Start: s.Start, End: s.End, Skip: skip, Method: method, Today: today}
}

// load picks the Invoice Ninja, its Sure (or Firefly) partner, their data
// and the space's tax settings.
func load(ctx context.Context, d *sql.DB, who *access.Principal, connID int64, fresh svcdata.Freshness) (book, []Choice, error) {
	views, err := connections.Listing(d, who, enums.RightUse)
	if err != nil {
		return book{}, nil, err
	}
	var choices []Choice
	for _, v := range views {
		if v.Service == enums.ServiceInvoiceNinja {
			choices = append(choices, Choice{ID: v.ID, Name: v.Name})
		}
	}
	if len(choices) == 0 {
		return book{}, nil, ErrNoNinja
	}
	st, err := loadState(d, who)
	if err != nil {
		return book{}, choices, err
	}
	if connID == 0 {
		connID = st.Conn
	}
	if !slices.ContainsFunc(choices, func(c Choice) bool { return c.ID == connID }) {
		connID = choices[0].ID
	}

	b := book{state: st}
	err = db.WithRead(d, func(tx *sql.Tx) error {
		if b.conn, err = content.Connection(tx, connID); err != nil || b.conn == nil {
			return err
		}
		if sp, err := content.Space(tx, b.conn.SpaceID); err == nil && sp != nil {
			b.settings = sp.Settings
		}
		at := verbund.Asker{SpaceID: b.conn.SpaceID, ConnID: b.conn.ID}
		for _, service := range []enums.ServiceType{enums.ServiceSure, enums.ServiceFirefly} {
			if b.bankConn, _, err = verbund.Partner(tx, who, at, service); err != nil || b.bankConn != nil {
				return err
			}
		}
		return nil
	})
	if err != nil || b.conn == nil {
		return b, choices, errors.Join(err, ErrNoNinja)
	}

	res, err := fetch(ctx, d, who, b.conn, fresh)
	if err != nil {
		return b, choices, err
	}
	if res.Error != "" {
		return b, choices, FetchError{res.Error}
	}
	if b.ninja, _ = res.Data.(*sources.NinjaDataset); b.ninja == nil {
		return b, choices, ErrNoBank
	}
	if b.bankConn != nil {
		if res, err := fetch(ctx, d, who, b.bankConn, fresh); err == nil {
			b.bank, _ = res.Data.(*sources.SureDataset)
		}
	}
	return b, choices, nil
}

// fetch reads a connection's dataset; a stored one the background run
// has not fetched yet is fetched now.
func fetch(ctx context.Context, d *sql.DB, who *access.Principal, conn *model.Connection, fresh svcdata.Freshness) (svcdata.Result, error) {
	key, holder := sources.DataKey(enums.ServiceType(conn.Service)), model.UserHolder(who.UserID)
	res, err := svcdata.Get(ctx, d, key, nil, conn, holder, fresh)
	if err == nil && res.Pending {
		return svcdata.Get(ctx, d, key, nil, conn, holder, svcdata.Cached)
	}
	return res, err
}

// FetchError is a service that could not be read, with its message.
type FetchError struct{ Msg string }

func (e FetchError) Error() string { return e.Msg }
