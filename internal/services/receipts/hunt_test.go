package receipts

import (
	"testing"
	"time"

	"andon/internal/services/assist"
	"andon/internal/services/mailfwd"
	"andon/internal/sources"
)

// A mail fits by amount or by sender, near the expense's day; Claude's
// reading beats the amount found in the text; exact amounts come first.
func TestMailHits(t *testing.T) {
	day := time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)
	mail := func(uid uint32, sender, domain string, amount float64, apart int) mailfwd.Item {
		return mailfwd.Item{MailConn: 5, Paperless: true, MailInvoice: sources.MailInvoice{UID: uid, Sender: sender, Domain: domain,
			Amount: amount, Date: day.AddDate(0, 0, apart)}}
	}
	read := mail(4, "Shop", "shop.de", 0, 1)
	read.Read = &assist.Invoice{Gross: 23.80}
	mails := []mailfwd.Item{
		mail(1, "Nordhost GmbH", "nordhost.de", 0, 3), // sender fits
		mail(2, "Mietwagen Nord", "mw.de", 50, 2),     // neither
		mail(3, "Shop", "shop.de", 23.80, -60),        // too far
		read,                                          // amount read by Claude
	}
	e := sources.ReceiptExpense{Key: "e1", Vendor: "Nordhost", Amount: 23.80, Day: "2026-09-10"}

	hits := mailHits(mails, e)
	if len(hits) != 2 || hits[0].UID != 4 || !hits[0].Exact || hits[1].UID != 1 || hits[1].Exact {
		t.Fatalf("hits: %+v", hits)
	}
	if hits[0].Ref() != "5:4" {
		t.Errorf("ref: %s", hits[0].Ref())
	}
}
