package receipts

// The hunt works the "to link" list as a workbench (services/workbench):
// per expense the user picks a scan, a combo or an invoice mail; the
// review spells the picks out, Apply writes them.
//
//	expense without scan ─┬─ scan(s) in Paperless ──► link both sides
//	                      └─ invoice mail ──────────► to Paperless; its
//	                         scan is offered for linking once Paperless
//	                         has read it

import (
	"context"
	"database/sql"
	"math"
	"slices"
	"strconv"
	"strings"
	"time"

	"andon/internal/services/access"
	"andon/internal/services/mailfwd"
	"andon/internal/services/svcdata"
	"andon/internal/services/workbench"
	"andon/internal/sources"
)

// The hunt's actions and fields, as the form sends them:
// act_Kx9=link docs_Kx9=12,13 · act_Kx9=mail mail_Kx9=5:4711.
const (
	ActLink   = "link"
	ActMail   = "mail"
	FieldDocs = "docs"
	FieldMail = "mail"
)

// HuntFields lists the fields, for the form.
var HuntFields = []string{FieldDocs, FieldMail}

const (
	ProblemGone  workbench.Problem = "receipts.problem_gone"  // linked or ignored meanwhile
	ProblemTaken workbench.Problem = "receipts.problem_taken" // a scan is no longer free
	ProblemMail  workbench.Problem = "receipts.problem_mail"  // the mail is gone or sent already
)

// mailDays is how far a mail may lie from its expense's day.
const mailDays = 45

// MailHit is an invoice mail that may carry an expense's scan.
type MailHit struct {
	Conn    int64
	UID     uint32
	Mailbox string
	Date    time.Time
	Sender  string
	Subject string
	Amount  float64 // read by Claude if asked, else found in the text; 0 = none
	Exact   bool    // the amount is the expense's
}

// Ref is the mail as the form names it: "5:4711".
func (m MailHit) Ref() string {
	return strconv.FormatInt(m.Conn, 10) + ":" + strconv.FormatUint(uint64(m.UID), 10)
}

// mailHits are the mails fitting expense e, exact amounts first: within
// mailDays of its day, with its amount or its vendor as sender.
func mailHits(mails []mailfwd.Item, e sources.ReceiptExpense) []MailHit {
	var out []MailHit
	for _, m := range mails {
		apart, ok := daysApart(e.Day, m.Date.Format(time.DateOnly))
		if !ok || apart > mailDays {
			continue
		}
		amount := m.Amount
		if m.Read != nil && m.Read.Gross > 0 {
			amount = m.Read.Gross
		}
		exact := amount > 0 && math.Abs(amount-e.Amount) <= amountTol
		vendor := e.Vendor != "" && tokenSetRatio(normalize(e.Vendor), normalize(m.Sender+" "+m.Domain)) >= vendorStrong
		if !exact && !vendor {
			continue
		}
		out = append(out, MailHit{Conn: m.MailConn, UID: m.UID, Mailbox: m.Mailbox, Date: m.Date, Sender: m.Sender,
			Subject: m.Subject, Amount: amount, Exact: exact})
	}
	slices.SortStableFunc(out, func(a, b MailHit) int {
		switch {
		case a.Exact && !b.Exact:
			return -1
		case b.Exact && !a.Exact:
			return 1
		}
		return b.Date.Compare(a.Date)
	})
	return out
}

// mailsOf are the invoice mails of the pair's Verbund: its mailbox, else
// every mailbox whose files go to the pair's Paperless.
func (p pair) mailsOf(ctx context.Context, d *sql.DB, who *access.Principal) []mailfwd.Item {
	mails, _, err := mailfwd.List(ctx, d, who)
	if err != nil {
		return nil
	}
	return slices.DeleteFunc(mails, func(m mailfwd.Item) bool {
		if p.mail != nil {
			return m.MailConn != p.mail.ID || m.ToDocs != p.docs.ID
		}
		return m.ToDocs != p.docs.ID
	})
}

// addMails gives each match its fitting mails; a mailbox that cannot
// be read leaves them out.
func (p pair) addMails(ctx context.Context, d *sql.DB, who *access.Principal, matches []ExpenseMatch) {
	mails := p.mailsOf(ctx, d, who)
	if len(mails) == 0 {
		return
	}
	for i := range matches {
		matches[i].Mails = mailHits(mails, matches[i].Expense)
	}
}

// HuntItem is what a step resolves to, as the review shows it.
type HuntItem struct {
	Expense sources.ReceiptExpense
	Docs    []sources.ReceiptDoc
	Mail    *MailHit
}

// HuntPlan is the review: what Apply would write.
type HuntPlan struct {
	workbench.Plan[HuntItem]
	Links
	Year int
}

// hunt is the loaded data of one year's hunt.
type hunt struct {
	pair
	expenses map[string]sources.ReceiptExpense // open
	docs     map[int64]sources.ReceiptDoc      // free
	mails    map[string]MailHit
	ctx      context.Context
	d        *sql.DB
	who      *access.Principal
	ip       string
}

// loadHunt reads the year's open expenses, free scans and invoice mails.
func loadHunt(ctx context.Context, d *sql.DB, who *access.Principal, year int) (*hunt, error) {
	p, err := openPair(d, who)
	if err != nil {
		return nil, err
	}
	expenses, docs, err := p.unlinked(ctx, d, who, year)
	if err != nil {
		return nil, err
	}
	h := &hunt{pair: p, expenses: map[string]sources.ReceiptExpense{}, docs: map[int64]sources.ReceiptDoc{}, mails: map[string]MailHit{},
		ctx: ctx, d: d, who: who}
	for _, e := range expenses {
		h.expenses[e.Key] = e
	}
	for _, doc := range docs {
		h.docs[doc.ID] = doc
	}
	for _, m := range p.mailsOf(ctx, d, who) {
		hit := MailHit{Conn: m.MailConn, UID: m.UID, Mailbox: m.Mailbox, Date: m.Date, Sender: m.Sender, Subject: m.Subject, Amount: m.Amount}
		h.mails[hit.Ref()] = hit
	}
	return h, nil
}

// Check resolves one step: the expense must still wait, its scans be
// free, its mail not sent yet.
func (h *hunt) Check(s *workbench.Step) (HuntItem, workbench.Problem) {
	var it HuntItem
	e, ok := h.expenses[s.Case]
	if !ok {
		return it, ProblemGone
	}
	it.Expense = e

	switch s.Action {
	case ActLink:
		ids := idsOf(s.Field(FieldDocs))
		if len(ids) == 0 {
			return it, ProblemTaken
		}
		for _, id := range ids {
			doc, free := h.docs[id]
			if !free {
				return it, ProblemTaken
			}
			it.Docs = append(it.Docs, doc)
		}
	case ActMail:
		m, found := h.mails[s.Field(FieldMail)]
		if !found {
			return it, ProblemMail
		}
		it.Mail = &m
	default:
		return it, ProblemGone
	}
	return it, workbench.ProblemNone
}

// idsOf reads a list of scan IDs: "12,13" (idList's form).
func idsOf(list string) []int64 {
	var out []int64
	for _, part := range strings.Split(list, ",") {
		if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil {
			out = append(out, id)
		}
	}
	return out
}

// Write links the scans, or sends the mail's files to Paperless.
func (h *hunt) Write(ctx context.Context, p workbench.Planned[HuntItem]) (string, error) {
	if p.Action == ActMail {
		_, err := mailfwd.Forward(ctx, h.d, h.who, p.Item.Mail.Conn, p.Item.Mail.UID, h.ip)
		return p.Item.Expense.Number, err
	}
	ids := make([]int64, 0, len(p.Item.Docs))
	for _, doc := range p.Item.Docs {
		ids = append(ids, doc.ID)
	}
	return Link(ctx, h.d, h.who, p.Item.Expense.Key, ids, h.ip)
}

func (h *hunt) Name(p workbench.Planned[HuntItem]) string {
	return strings.TrimSpace(p.Item.Expense.Number + " " + p.Item.Expense.Vendor)
}

func (h *hunt) Reason(err error) string { return err.Error() }

// Audit writes none: Link and Forward log their own.
func (h *hunt) Audit(workbench.Planned[HuntItem]) map[string]any { return nil }

// Review checks the picks against the current data and spells them out.
func Review(ctx context.Context, d *sql.DB, who *access.Principal, year int, steps []workbench.Step) (HuntPlan, error) {
	h, err := loadHunt(ctx, d, who, year)
	if err != nil {
		return HuntPlan{}, err
	}
	plan, err := workbench.PlanOf[HuntItem](h, steps)
	return HuntPlan{Plan: plan, Links: h.links(), Year: year}, err
}

// Apply writes the confirmed picks against fresh data.
func Apply(ctx context.Context, d *sql.DB, who *access.Principal, year int, steps []workbench.Step, ip string) (workbench.Result, error) {
	p, err := openPair(d, who)
	if err != nil {
		return workbench.Result{}, err
	}
	svcdata.Forget(p.ninja.ID)
	svcdata.Forget(p.docs.ID)

	h, err := loadHunt(ctx, d, who, year)
	if err != nil {
		return workbench.Result{}, err
	}
	h.ip = ip
	plan, err := workbench.PlanOf[HuntItem](h, steps)
	if err != nil {
		return workbench.Result{}, err
	}
	if plan.Writes() > 0 && sources.IsDemo(h.ninja.URL) {
		return workbench.Result{}, ErrDemo
	}
	return workbench.Apply[HuntItem](ctx, d, who, h, plan, "receipts", ip), nil
}
