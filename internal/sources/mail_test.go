package sources_test

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/backend/memory"
	"github.com/emersion/go-imap/server"

	"andon/internal/sources"
)

func TestInvoiceAmount(t *testing.T) {
	cases := map[string]float64{
		"Rechnungsbetrag: 1.234,56 € zzgl. Versand 4,90 €": 1234.56,
		"Total due: EUR 1,234.56":                          1234.56,
		"Artikel 12,00 € und 30,00 €":                      30,
		"keine Beträge":                                    0,
	}
	for text, want := range cases {
		if got := sources.InvoiceAmount(text); got != want {
			t.Errorf("%q = %v, want %v", text, got, want)
		}
	}
}

const invoiceMail = "From: Hetzner Online GmbH <billing@hetzner.com>\r\n" +
	"To: username@example.org\r\n" +
	"Subject: Ihre Rechnung R0012345\r\n" +
	"Date: %DATE%\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/mixed; boundary=XX\r\n" +
	"\r\n" +
	"--XX\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"Content-Transfer-Encoding: quoted-printable\r\n" +
	"\r\n" +
	"Guten Tag,=0D=0Ader Rechnungsbetrag von 1.234,56 =E2=82=AC wird abgebucht.\r\n" +
	"--XX\r\n" +
	"Content-Type: application/pdf; name=\"Rechnung_R0012345.pdf\"\r\n" +
	"Content-Disposition: attachment; filename=\"Rechnung_R0012345.pdf\"\r\n" +
	"Content-Transfer-Encoding: base64\r\n" +
	"\r\n" +
	"JVBERi0xLjQK\r\n" +
	"--XX--\r\n"

func TestMailFindsInvoiceOverIMAP(t *testing.T) {
	be := memory.New()
	user, _ := be.Login(nil, "username", "password")
	box, _ := user.GetMailbox("INBOX")
	body := strings.ReplaceAll(invoiceMail, "%DATE%", time.Now().UTC().Format(time.RFC1123Z))
	if err := box.CreateMessage(nil, time.Now(), strings.NewReader(body)); err != nil {
		t.Fatal(err)
	}

	srv := server.New(be)
	srv.AllowInsecureAuth = true
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)

	out, err := sources.MailData.Fetch(context.Background(), sources.Ctx{URL: "imap://127.0.0.1:" + port + "/INBOX", Secret: "username:password"})
	if err != nil {
		t.Fatal(err)
	}
	d := out.(*sources.MailDataset)
	if d.Scanned != 2 || len(d.Invoices) != 1 {
		t.Fatalf("data: %+v", d)
	}
	inv := d.Invoices[0]
	if inv.Amount != 1234.56 || inv.Domain != "hetzner.com" || inv.Sender != "Hetzner Online GmbH" || len(inv.Attachments) != 1 {
		t.Fatalf("invoice: %+v", inv)
	}
}

func TestMailFilesDownloadsPDF(t *testing.T) {
	be := memory.New()
	user, _ := be.Login(nil, "username", "password")
	box, _ := user.GetMailbox("INBOX")
	body := strings.ReplaceAll(invoiceMail, "%DATE%", time.Now().UTC().Format(time.RFC1123Z))
	if err := box.CreateMessage(nil, time.Now(), strings.NewReader(body)); err != nil {
		t.Fatal(err)
	}
	srv := server.New(be)
	srv.AllowInsecureAuth = true
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	sctx := sources.Ctx{URL: "imap://" + l.Addr().String() + "/INBOX", Secret: "username:password"}

	data, err := sources.MailData.Fetch(context.Background(), sctx)
	if err != nil {
		t.Fatal(err)
	}
	uid := data.(*sources.MailDataset).Invoices[0].UID
	files, err := sources.MailFiles(context.Background(), sctx, uid)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || files[0].Name != "Rechnung_R0012345.pdf" || string(files[0].Content) != "%PDF-1.4\n" {
		t.Fatalf("files: %+v", files)
	}
}

// plainMail is a one-part mail from sender with subject and body.
func plainMail(sender, subject, body string, at time.Time) string {
	return "From: " + sender + "\r\nTo: username@example.org\r\nSubject: " + subject + "\r\nDate: " + at.Format(time.RFC1123Z) +
		"\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + body + "\r\n"
}

// fileMail is plainMail with one attachment named file.
func fileMail(sender, subject, body, file string, at time.Time) string {
	return "From: " + sender + "\r\nTo: username@example.org\r\nSubject: " + subject + "\r\nDate: " + at.Format(time.RFC1123Z) +
		"\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=XX\r\n\r\n--XX\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + body +
		"\r\n--XX\r\nContent-Type: application/octet-stream; name=\"" + file + "\"\r\nContent-Disposition: attachment; filename=\"" + file +
		"\"\r\nContent-Transfer-Encoding: base64\r\n\r\nJVBERi0xLjQK\r\n--XX--\r\n"
}

// TestMailSkipsRefunds: a credit note, a refund or a receipt of a
// withdrawal is no invoice to record. A thread counts once, as its
// original: replies drop out and lend only an amount, and their PDF when
// the original has none (a signature image is no PDF). Monthly bills
// under the same subject stay apart; a reply listed before its original
// changes nothing.
func TestMailSkipsRefunds(t *testing.T) {
	be := memory.New()
	user, _ := be.Login(nil, "username", "password")
	box, _ := user.GetMailbox("INBOX")
	now := time.Now().UTC()
	day := func(n int) time.Time { return now.AddDate(0, 0, n) }
	mara, nord := "Mara Lind <mara@example.org>", "Kunde Nord <buero@example.org>"
	mails := []string{
		plainMail("Model Labs <billing@example.com>", "Credit note from Model Labs for invoice #A1", "Amount 20,00 €", day(-5)),
		plainMail("Model Labs <billing@example.com>", "Your refund from Model Labs #B2", "Refund 10,00 €", day(-5)),
		plainMail("Phone Shop <shop@example.net>", "Acknowledgement of receipt of your withdrawal – Reference W-1", "", day(-4)),
		fileMail(mara, "Deine Rechnung - Workshop", "anbei", "Rechnung_Workshop.pdf", day(-3)),
		fileMail(mara, "Re: Deine Rechnung - Workshop", "Betrag 480,00 €", "image001.png", day(-2)),
		plainMail(mara, "AW: Re: Deine Rechnung - Workshop", "danke", day(-1)),
		plainMail(nord, "Rechnung 77", "Betrag 50,00 €", day(-6)),
		fileMail(nord, "Re: Rechnung 77", "hier das PDF", "Rechnung_77.pdf", day(-5)),
		plainMail(nord, "Re: Rechnung 88", "danke", day(-2)),
		fileMail(nord, "Rechnung 88", "Betrag 60,00 €", "Rechnung_88.pdf", day(-4)),
		plainMail("Mobilfunk <rechnung@example.de>", "Ihre Online-Rechnung", "Rechnungsbetrag 21,50 €", day(-31)),
		plainMail("Mobilfunk <rechnung@example.de>", "Ihre Online-Rechnung", "Rechnungsbetrag 19,99 €", day(-1)),
	}
	for _, m := range mails {
		if err := box.CreateMessage(nil, now, strings.NewReader(m)); err != nil {
			t.Fatal(err)
		}
	}
	srv := server.New(be)
	srv.AllowInsecureAuth = true
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(l)
	t.Cleanup(func() { srv.Close() })
	sctx := sources.Ctx{URL: "imap://" + l.Addr().String() + "/INBOX", Secret: "username:password"}

	out, err := sources.MailData.Fetch(context.Background(), sctx)
	if err != nil {
		t.Fatal(err)
	}

	// Subject, amount and the PDF MailFiles finds under the entry's UID.
	var got []string
	for _, inv := range out.(*sources.MailDataset).Invoices {
		files, err := sources.MailFiles(context.Background(), sctx, inv.UID)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, f := range files {
			names = append(names, f.Name)
		}
		got = append(got, inv.Subject+" "+strconv.FormatFloat(inv.Amount, 'f', 2, 64)+" "+strings.Join(names, ","))
	}
	want := []string{"Ihre Online-Rechnung 21.50 ", "Rechnung 77 50.00 Rechnung_77.pdf", "Rechnung 88 60.00 Rechnung_88.pdf",
		"Deine Rechnung - Workshop 480.00 Rechnung_Workshop.pdf", "Ihre Online-Rechnung 19.99 "}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("invoices:\n %q\nwant\n %q", got, want)
	}
}
