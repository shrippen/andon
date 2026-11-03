// Package mailfwd hands invoice mails to Paperless.
//
//	List:    invoice mails from the last background run, per mailbox,
//	         with the Paperless connection of the same space
//	Forward: USE on both connections → download the mail's PDF/image
//	         attachments → Paperless consumes each one
//	Read:    on request, Claude reads the attachments' invoice fields;
//	         Forward then titles the documents "Vendor Number"
//	Dismiss: "no invoice": hides the mail from the list (Restore undoes)
//
// Marks (read, sent, no invoice) are stored per holder and mail key: a
// personal mailbox is each holder's own, UIDs of two of them overlap.
//
// Forward and Dismiss teach the mailbox's model (purpose "mail"); List
// marks and moves last what it holds for no invoice, never hides it.
package mailfwd

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"time"

	"andon/internal/bayes"
	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/model"
	"andon/internal/outbound"
	"andon/internal/repos/content"
	repodata "andon/internal/repos/data"
	"andon/internal/services/access"
	"andon/internal/services/assist"
	auditsvc "andon/internal/services/audit"
	"andon/internal/services/boards"
	"andon/internal/services/connections"
	"andon/internal/services/learn"
	"andon/internal/services/svcdata"
	"andon/internal/services/verbund"
	"andon/internal/sources"
)

var (
	// ErrNoPaperless means the mailbox's space has no Paperless connection.
	ErrNoPaperless = errors.New("mailfwd.no_paperless")
	// ErrNoFiles means the mail carries no PDF or image.
	ErrNoFiles = errors.New("mailfwd.no_files")
	// ErrDemo means a demo connection, which takes no writes.
	ErrDemo = errors.New("mailfwd.demo")
	// ErrNoMail means the mail is no longer in the mailbox's last run.
	ErrNoMail = errors.New("mailfwd.no_mail")
)

// PurposeMail is the model telling invoice mails from others.
const PurposeMail = "mail"

func init() {
	learn.Register(learn.Purpose{Key: PurposeMail, Service: enums.ServiceMail})
}

// LoginError names the connection whose personal login is missing, so
// the page can link to where the caller enters it.
type LoginError struct {
	ConnID int64
}

func (e *LoginError) Error() string { return "credential.missing" }

// loginOf turns a missing personal login on conn into a LoginError.
func loginOf(conn *model.Connection, err error) error {
	if errors.Is(err, svcdata.ErrMissingCredential) {
		return &LoginError{ConnID: conn.ID}
	}
	return err
}

// Item is one invoice mail that can be forwarded.
type Item struct {
	MailConn  int64
	Mailbox   string
	Paperless bool            // the mailbox has a Paperless to send to
	ToDocs    int64           // that Paperless
	Read      *assist.Invoice // nil until read
	Unlikely  bool            // the model holds it for no invoice
	sources.MailInvoice
}

// mailboxes returns the mail connections who may use, with their
// Paperless partner (nil if none, or several without a Verbund).
func mailboxes(d *sql.DB, who *access.Principal) (map[*model.Connection]*model.Connection, error) {
	out := map[*model.Connection]*model.Connection{}
	views, err := connections.Listing(d, who, enums.RightUse)
	if err != nil {
		return nil, err
	}
	err = db.WithRead(d, func(tx *sql.Tx) error {
		for _, v := range views {
			if v.Service != enums.ServiceMail {
				continue
			}
			mail, err := content.Connection(tx, v.ID)
			if err != nil || mail == nil {
				return err
			}
			// The mailbox's Paperless partner: Verbund, or the one there is.
			paperless, _, err := verbund.Partner(tx, who, verbund.Asker{SpaceID: mail.SpaceID, ConnID: mail.ID}, enums.ServicePaperless)
			if err != nil {
				return err
			}
			out[mail] = paperless
		}
		return nil
	})
	return out, err
}

// Marks in a mail's stored read fields, so the billing list can leave
// it out: sent to Paperless from Andon, or set aside as no invoice.
const (
	forwardedKey = "forwarded"
	dismissedKey = "dismissed"
)

// isForwarded reports whether a mail's stored fields mark it as sent.
func isForwarded(fields map[string]any) bool {
	_, ok := fields[forwardedKey]
	return ok
}

// isDismissed reports whether the mail was set aside as no invoice.
func isDismissed(fields map[string]any) bool {
	_, ok := fields[dismissedKey]
	return ok
}

// hasRead reports whether Claude read invoice fields from the mail
// (fields beyond the marks).
func hasRead(fields map[string]any) bool {
	for k := range fields {
		if k != forwardedKey && k != dismissedKey {
			return true
		}
	}
	return false
}

// List returns the invoice mails of every usable mailbox, without the
// ones already sent to Paperless (sent counts those) or set aside; the
// ones the model holds for no invoice come last, marked.
func List(ctx context.Context, d *sql.DB, who *access.Principal) (items []Item, sent int, err error) {
	boxes, err := mailboxes(d, who)
	if err != nil {
		return nil, 0, err
	}
	var out []Item
	for mail, paperless := range boxes {
		data := lastRun(ctx, d, who, mail)
		if data == nil {
			continue
		}
		reads, err := marksOf(d, mail, who)
		if err != nil {
			return nil, 0, err
		}
		use := learn.Model(d, PurposeMail, mail, model.UserHolder(who.UserID))
		for _, inv := range data.Invoices {
			fields := reads.of(inv)
			if isForwarded(fields) {
				sent++
				continue
			}
			if isDismissed(fields) {
				continue
			}
			item := Item{MailConn: mail.ID, Mailbox: mail.Name, Paperless: paperless != nil, MailInvoice: inv,
				Unlikely: metrics.MailUnlikely(use, inv)}
			if paperless != nil {
				item.ToDocs = paperless.ID
			}
			if hasRead(fields) {
				item.Read = invoiceOf(fields)
			}
			out = append(out, item)
		}
	}

	// Likely invoices first; within each, the order of the mailbox.
	slices.SortStableFunc(out, func(a, b Item) int {
		switch {
		case a.Unlikely == b.Unlikely:
			return 0
		case b.Unlikely:
			return -1
		}
		return 1
	})
	return out, sent, nil
}

// lastRun is mail's dataset of the last background run; nil before one.
func lastRun(ctx context.Context, d *sql.DB, who *access.Principal, mail *model.Connection) *sources.MailDataset {
	res, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceMail), nil, mail, model.UserHolder(who.UserID), svcdata.Stored)
	if err != nil {
		return nil
	}
	data, _ := res.Data.(*sources.MailDataset)
	return data
}

// teach lets mail's model learn that inv is of kind k.
func teach(d *sql.DB, who *access.Principal, mail *model.Connection, inv sources.MailInvoice, k metrics.MailKind) error {
	return learn.Learn(d, PurposeMail, mail, model.UserHolder(who.UserID), func(m *bayes.Model) bool {
		return metrics.MailLearn(m, inv, k)
	})
}

// listed is mail uid as who's last run found it.
func listed(ctx context.Context, d *sql.DB, who *access.Principal, mail *model.Connection, uid uint32) (sources.MailInvoice, error) {
	data := lastRun(ctx, d, who, mail)
	if data == nil {
		return sources.MailInvoice{}, ErrNoMail
	}
	i := slices.IndexFunc(data.Invoices, func(inv sources.MailInvoice) bool { return inv.UID == uid })
	if i < 0 {
		return sources.MailInvoice{}, ErrNoMail
	}
	return data.Invoices[i], nil
}

// marks are a mailbox's stored reads as one holder sees them.
type marks struct {
	holder model.Holder
	rows   []repodata.MailRead
}

// marksOf reads mail's marks for who: theirs, if the login is personal.
func marksOf(q db.Queryer, mail *model.Connection, who *access.Principal) (marks, error) {
	rows, err := repodata.MailReads(q, mail.ID)
	return marks{holder: svcdata.HolderOf(mail, model.UserHolder(who.UserID)), rows: rows}, err
}

// of is a copy of inv's fields; nil if none.
func (m marks) of(inv sources.MailInvoice) map[string]any {
	return repodata.ReadOf(m.rows, m.holder, inv.UID, inv.Key())
}

// mark changes inv's stored fields for who, in one transaction.
func mark(d *sql.DB, who *access.Principal, mail *model.Connection, inv sources.MailInvoice, change func(fields map[string]any)) error {
	return db.WithTx(d, func(tx *sql.Tx) error {
		m, err := marksOf(tx, mail, who)
		if err != nil {
			return err
		}
		fields := m.of(inv)
		if fields == nil {
			fields = map[string]any{}
		}
		change(fields)
		return repodata.SaveMailRead(tx, mail.ID, repodata.MailRead{Holder: m.holder, UID: inv.UID, Mail: inv.Key(), Fields: fields})
	})
}

// Dismiss sets mail uid aside as no invoice: the list leaves it out, and
// the mailbox's model learns from it.
func Dismiss(ctx context.Context, d *sql.DB, who *access.Principal, mailConnID int64, uid uint32, ip string) error {
	mail, inv, err := find(ctx, d, who, mailConnID, uid)
	if err != nil {
		return err
	}
	if err := teach(d, who, mail, inv, metrics.MailNoInvoice); err != nil {
		return err
	}
	err = mark(d, who, mail, inv, func(f map[string]any) { f[dismissedKey] = time.Now().UTC().Format(time.RFC3339) })
	if err != nil {
		return err
	}
	return auditsvc.Log(d, &who.UserID, "mail.dismiss", fmt.Sprintf("%s#%d", mail.Name, uid), ip, nil)
}

// Restore undoes Dismiss: the mail is listed again, the model forgets
// the decision. A mail not set aside is left as it is (its decision may
// be a forward).
func Restore(ctx context.Context, d *sql.DB, who *access.Principal, mailConnID int64, uid uint32, ip string) error {
	mail, inv, err := find(ctx, d, who, mailConnID, uid)
	if err != nil {
		return err
	}
	m, err := marksOf(d, mail, who)
	if err != nil || !isDismissed(m.of(inv)) {
		return err
	}
	err = learn.Learn(d, PurposeMail, mail, model.UserHolder(who.UserID), func(m *bayes.Model) bool { return metrics.MailForget(m, inv) })
	if err != nil {
		return err
	}
	if err := mark(d, who, mail, inv, func(f map[string]any) { delete(f, dismissedKey) }); err != nil {
		return err
	}
	return auditsvc.Log(d, &who.UserID, "mail.restore", fmt.Sprintf("%s#%d", mail.Name, uid), ip, nil)
}

// find is mailbox id, if who may use it, and its mail uid.
func find(ctx context.Context, d *sql.DB, who *access.Principal, id int64, uid uint32) (*model.Connection, sources.MailInvoice, error) {
	mail, err := mailbox(d, who, id)
	if err != nil {
		return nil, sources.MailInvoice{}, err
	}
	inv, err := listed(ctx, d, who, mail, uid)
	return mail, inv, err
}

// mailbox is the mail connection id, if who may use it.
func mailbox(d *sql.DB, who *access.Principal, id int64) (*model.Connection, error) {
	boxes, err := mailboxes(d, who)
	if err != nil {
		return nil, err
	}
	for m := range boxes {
		if m.ID == id {
			return m, nil
		}
	}
	return nil, access.ErrDenied
}

// Forward sends the attachments of one mail to Paperless; returns how
// many files went.
func Forward(ctx context.Context, d *sql.DB, who *access.Principal, mailConnID int64, uid uint32, ip string) (int, error) {
	boxes, err := mailboxes(d, who)
	if err != nil {
		return 0, err
	}
	var mail, paperless *model.Connection
	for m, p := range boxes {
		if m.ID == mailConnID {
			mail, paperless = m, p
		}
	}
	if mail == nil {
		return 0, access.ErrDenied
	}
	if paperless == nil {
		return 0, ErrNoPaperless
	}
	if err := connections.Writable(d, who, paperless.ID); err != nil {
		return 0, err
	}

	// The own Paperless login first: no mail is fetched without it.
	token, err := svcdata.Secret(ctx, d, paperless, model.UserHolder(who.UserID))
	if err != nil {
		return 0, loginOf(paperless, err)
	}
	if sources.IsDemo(mail.URL) || sources.IsDemo(paperless.URL) {
		return 0, ErrDemo
	}
	inv, err := listed(ctx, d, who, mail, uid)
	if err != nil {
		return 0, err
	}
	files, err := mailFiles(ctx, d, who, mail, uid)
	if err != nil {
		return 0, err
	}
	title := ""
	if m, err := marksOf(d, mail, who); err == nil && hasRead(m.of(inv)) {
		title = invoiceOf(m.of(inv)).Title()
	}
	for _, f := range files {
		if _, err := outbound.PaperlessUpload(ctx, outbound.Target{URL: paperless.URL, Token: token, VerifyTLS: paperless.VerifyTLS}, f.Name, title, f.Content); err != nil {
			return 0, err
		}
	}
	svcdata.Forget(paperless.ID)
	if err := mark(d, who, mail, inv, func(f map[string]any) { f[forwardedKey] = time.Now().UTC().Format(time.RFC3339) }); err != nil {
		return 0, err
	}

	// Sent is done: a model that cannot learn only says so.
	if err := teach(d, who, mail, inv, metrics.MailIsInvoice); err != nil {
		slog.Warn("mailfwd: learn", "conn", mail.ID, "uid", uid, "err", err)
	}
	return len(files), auditsvc.Log(d, &who.UserID, "mail.to_paperless", fmt.Sprintf("%s#%d", mail.Name, uid), ip,
		map[string]any{"files": len(files)})
}

// mailFiles downloads one mail's attachments; USE on the mailbox is
// checked by the caller.
func mailFiles(ctx context.Context, d *sql.DB, who *access.Principal, mail *model.Connection, uid uint32) ([]sources.MailFile, error) {
	sctx, err := svcdata.SourceCtx(d, mail, model.UserHolder(who.UserID))
	if err != nil {
		return nil, loginOf(mail, err)
	}
	files, err := sources.MailFiles(ctx, sctx, uid)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, ErrNoFiles
	}
	return files, nil
}

// Read lets Claude read the invoice fields of one mail's attachments and
// keeps them for the list and the Paperless title.
func Read(ctx context.Context, d *sql.DB, who *access.Principal, mailConnID int64, uid uint32, ip string) (assist.Invoice, error) {
	boxes, err := mailboxes(d, who)
	if err != nil {
		return assist.Invoice{}, err
	}
	var mail *model.Connection
	for m := range boxes {
		if m.ID == mailConnID {
			mail = m
		}
	}
	if mail == nil {
		return assist.Invoice{}, access.ErrDenied
	}
	listedInv, err := listed(ctx, d, who, mail, uid)
	if err != nil {
		return assist.Invoice{}, err
	}
	files, err := mailFiles(ctx, d, who, mail, uid)
	if err != nil {
		return assist.Invoice{}, err
	}
	var readable []outbound.LLMFile
	for _, f := range files {
		if media := http.DetectContentType(f.Content); outbound.LLMReadable(media) {
			readable = append(readable, outbound.LLMFile{Media: media, Content: f.Content})
		}
	}
	if len(readable) == 0 {
		return assist.Invoice{}, ErrNoFiles
	}
	inv, err := assist.ReadInvoice(ctx, readable)
	if err != nil {
		return assist.Invoice{}, err
	}

	read, err := fieldsOf(inv)
	if err != nil {
		return assist.Invoice{}, err
	}
	if err := mark(d, who, mail, listedInv, func(f map[string]any) { maps.Copy(f, read) }); err != nil {
		return assist.Invoice{}, err
	}
	return inv, auditsvc.Log(d, &who.UserID, "mail.read", fmt.Sprintf("%s#%d", mail.Name, uid), ip, nil)
}

// fieldsOf is inv as stored fields.
func fieldsOf(inv assist.Invoice) (map[string]any, error) {
	out := map[string]any{}
	raw, err := json.Marshal(inv)
	if err == nil {
		err = json.Unmarshal(raw, &out)
	}
	return out, err
}

// invoiceOf reads a stored read back; a read that does not decode
// shows as empty rather than failing the list.
func invoiceOf(fields map[string]any) *assist.Invoice {
	var inv assist.Invoice
	raw, err := json.Marshal(fields)
	if err == nil {
		err = json.Unmarshal(raw, &inv)
	}
	if err != nil {
		slog.Warn("mailfwd: stored read", "err", err)
	}
	return &inv
}

// ErrNoFile: the mail has no attachment at that position.
var ErrNoFile = errors.New("mail.no_file")

// File is one attachment of a mail behind a placed tile, for a preview in
// its dialog: the viewer must see the tile and may use its mailbox.
func File(ctx context.Context, d *sql.DB, who *access.Principal, placementID int64, uid uint32, n int) (sources.MailFile, error) {
	w, err := boards.PlacedWidget(d, who, placementID)
	if err != nil {
		return sources.MailFile{}, err
	}
	if w.ConnectionID == nil {
		return sources.MailFile{}, ErrNoFile
	}
	if _, err := connections.Get(d, who, *w.ConnectionID); err != nil {
		return sources.MailFile{}, err
	}
	mail, err := connections.ByID(d, *w.ConnectionID)
	if err != nil {
		return sources.MailFile{}, err
	}
	files, err := mailFiles(ctx, d, who, mail, uid)
	if err != nil {
		return sources.MailFile{}, err
	}
	if n < 0 || n >= len(files) {
		return sources.MailFile{}, ErrNoFile
	}
	return files[n], nil
}
