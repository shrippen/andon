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

// The next mail of a sender left before is suggested as no invoice, one
// forwarded as invoice; an unknown sender, or one sharing only words,
// gets no suggestion.
func TestMailSuggest(t *testing.T) {
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
	if got := MailSuggest(u, next); got != MailNoInvoice {
		t.Fatalf("next mail of a sender left before: %q", got)
	}
	next = bill
	next.UID = 10
	if got := MailSuggest(u, next); got != MailIsInvoice {
		t.Fatalf("next mail of a forwarded sender: %q", got)
	}
	if MailSuggest(u, stranger) != MailNoKind || MailSuggest(u, words) != MailNoKind {
		t.Fatal("an unknown sender or shared words get a suggestion")
	}
	if MailSuggest(bayes.Use{}, news) != MailNoKind {
		t.Fatal("learning off suggests")
	}

	// Forwarded after all: the decision replaces the earlier one.
	if !MailLearn(m, news, MailIsInvoice) || m.Stats().Examples != 2 || MailSuggest(u, news) == MailNoInvoice {
		t.Fatalf("a changed decision does not replace the example: %+v", m.Stats())
	}

	// Undone: the example goes.
	if !MailForget(m, news) || m.Stats().Examples != 1 {
		t.Fatalf("undo keeps the example: %+v", m.Stats())
	}
}
