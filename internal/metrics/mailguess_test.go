package metrics

import (
	"slices"
	"testing"

	"andon/internal/bayes"
	"andon/internal/sources"
)

// Features: sender address and domain, words of name, subject and
// attachments; no extension, keyword or number.
func TestMailFeatures(t *testing.T) {
	inv := sources.MailInvoice{UID: 7, Sender: "Nordhost Server", Addr: "Billing@Nordhost.example", Domain: "nordhost.example",
		Subject: "Ihre Rechnung R0012345", Attachments: []string{"Rechnung_R0012345.pdf", "AGB.pdf"}}
	want := []string{"f:billing@nordhost.example", "d:nordhost.example", "w:nordhost", "w:server", "w:ihre", "w:agb"}
	if got := mailFeatures(inv); !slices.Equal(got, want) {
		t.Fatalf("features %v, want %v", got, want)
	}
}

// A sender left before is likely no invoice; one forwarded, one the
// model knows nothing of, or one sharing only words, is not marked.
func TestMailUnlikely(t *testing.T) {
	news := sources.MailInvoice{UID: 1, Sender: "Shop", Addr: "news@shop.example", Domain: "shop.example", Subject: "Ihre Rechnung zum Newsletter"}
	bill := sources.MailInvoice{UID: 2, Sender: "Nordhost", Addr: "billing@nordhost.example", Domain: "nordhost.example", Subject: "Ihre Rechnung"}
	stranger := sources.MailInvoice{UID: 3, Sender: "Farbraum", Addr: "sales@farbraum.example", Domain: "farbraum.example", Subject: "Invoice"}
	// Shares "Ihre" and "Newsletter" with the one left, not the sender.
	words := sources.MailInvoice{UID: 4, Sender: "Kanzlei", Addr: "post@kanzlei.example", Domain: "kanzlei.example", Subject: "Ihre Rechnung zum Newsletter"}

	m := bayes.New()
	MailLearn(m, news, MailNoInvoice)
	MailLearn(m, bill, MailIsInvoice)
	u := bayes.Use{Model: m, MinP: 0.6}

	next := news
	next.UID = 9
	if !MailUnlikely(u, next) {
		t.Fatal("the next mail of a sender left before is not marked")
	}
	next = bill
	next.UID = 10
	if MailUnlikely(u, next) || MailUnlikely(u, stranger) || MailUnlikely(u, words) {
		t.Fatal("a forwarded sender, an unknown one or shared words mark a mail")
	}
	if MailUnlikely(bayes.Use{}, news) {
		t.Fatal("learning off marks a mail")
	}

	// Forwarded after all: the decision replaces the earlier one.
	if !MailLearn(m, news, MailIsInvoice) || m.Stats().Examples != 2 || MailUnlikely(u, news) {
		t.Fatalf("a changed decision does not replace the example: %+v", m.Stats())
	}

	// Undone: the example goes.
	if !MailForget(m, news) || m.Stats().Examples != 1 {
		t.Fatalf("undo keeps the example: %+v", m.Stats())
	}
}
