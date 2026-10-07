// Package mailfwd hands invoice mails to Paperless.
//
//	List:    invoice mails from the last background run, per mailbox,
//	         with the Paperless connection of the same space
//	Forward: USE on both connections → download the mail's PDF/image
//	         attachments → Paperless consumes each one
//	Read:    on request, Claude reads the attachments' invoice fields;
//	         Forward then titles the documents "Vendor Number"
package mailfwd

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/outbound"
	"andon/internal/repos/content"
	repodata "andon/internal/repos/data"
	"andon/internal/services/access"
	"andon/internal/services/assist"
	auditsvc "andon/internal/services/audit"
	"andon/internal/services/boards"
	"andon/internal/services/connections"
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
)

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
	Paperless bool
	Read      *assist.Invoice // nil until read
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

// forwardedKey marks a mail sent to Paperless from Andon in its stored
// read fields, so the billing list can leave it out.
const forwardedKey = "forwarded"

// isForwarded reports whether a mail's stored fields mark it as sent.
func isForwarded(fields map[string]any) bool {
	_, ok := fields[forwardedKey]
	return ok
}

// hasRead reports whether Claude read invoice fields from the mail
// (fields beyond the forwarded mark).
func hasRead(fields map[string]any) bool {
	for k := range fields {
		if k != forwardedKey {
			return true
		}
	}
	return false
}

// List returns the invoice mails of every usable mailbox, without the
// ones already sent to Paperless; sent counts those.
func List(ctx context.Context, d *sql.DB, who *access.Principal) (items []Item, sent int, err error) {
	boxes, err := mailboxes(d, who)
	if err != nil {
		return nil, 0, err
	}
	uid := who.UserID
	var out []Item
	for mail, paperless := range boxes {
		res, err := svcdata.Get(ctx, d, sources.DataKey(enums.ServiceMail), nil, mail, model.UserHolder(uid), svcdata.Stored)
		if err != nil {
			continue
		}
		data, ok := res.Data.(*sources.MailDataset)
		if !ok {
			continue
		}
		reads, err := repodata.MailReads(d, mail.ID)
		if err != nil {
			return nil, 0, err
		}
		for _, inv := range data.Invoices {
			fields := reads[inv.UID]
			if isForwarded(fields) {
				sent++
				continue
			}
			item := Item{MailConn: mail.ID, Mailbox: mail.Name, Paperless: paperless != nil, MailInvoice: inv}
			if hasRead(fields) {
				item.Read = invoiceOf(fields)
			}
			out = append(out, item)
		}
	}
	return out, sent, nil
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
	files, err := mailFiles(ctx, d, who, mail, uid)
	if err != nil {
		return 0, err
	}
	title := ""
	fields := map[string]any{}
	if reads, err := repodata.MailReads(d, mail.ID); err == nil && reads[uid] != nil {
		fields = reads[uid]
		if hasRead(fields) {
			title = invoiceOf(fields).Title()
		}
	}
	for _, f := range files {
		if _, err := outbound.PaperlessUpload(ctx, outbound.Target{URL: paperless.URL, Token: token, VerifyTLS: paperless.VerifyTLS}, f.Name, title, f.Content); err != nil {
			return 0, err
		}
	}
	svcdata.Forget(paperless.ID)
	fields[forwardedKey] = time.Now().UTC().Format(time.RFC3339)
	if err := db.WithTx(d, func(tx *sql.Tx) error { return repodata.SaveMailRead(tx, mail.ID, uid, fields) }); err != nil {
		return 0, err
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

	if err := db.WithTx(d, func(tx *sql.Tx) error { return repodata.SaveMailRead(tx, mail.ID, uid, inv) }); err != nil {
		return assist.Invoice{}, err
	}
	return inv, auditsvc.Log(d, &who.UserID, "mail.read", fmt.Sprintf("%s#%d", mail.Name, uid), ip, nil)
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
