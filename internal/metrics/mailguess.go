package metrics

import (
	"path"
	"strings"

	"andon/internal/bayes"
	"andon/internal/sources"
)

// Is a listed invoice mail an invoice? Like a spam filter: a naive Bayes
// model learns what the user forwards (invoice) and leaves (no invoice).
//
//	mail ──► features ──────────────────► bayes.Model
//	  f:billing@nordhost.example  sender   │
//	  d:nordhost.example          domain   ▼
//	  w:nordhost w:ihre …         words    Guess ≥ MinP → "no invoice"
//
// The keyword filter of the source still picks the candidates; the model
// only ranks and marks them, it never hides one. Only the sender is
// evidence: words ("ihre", "your") are shared by invoices and others
// alike, they only weigh in.

// MailKind is what the user decided a mail is: the model's label.
type MailKind string

const (
	MailIsInvoice MailKind = "invoice"
	MailNoInvoice MailKind = "other"
)

// Feature prefixes of a mail: its sender's address and domain; words
// share wordFeature with the EÜR model.
const (
	addrFeature   = "f:"
	domainFeature = "d:"
)

// MailLearn teaches m that inv is of kind k; a mail decided again
// replaces its earlier example. It says whether m changed.
func MailLearn(m *bayes.Model, inv sources.MailInvoice, k MailKind) bool {
	return m.Learn(mailID(inv), string(k), mailFeatures(inv))
}

// MailForget takes back what m learned from inv (a decision undone).
func MailForget(m *bayes.Model, inv sources.MailInvoice) bool {
	return m.Forget(mailID(inv))
}

// MailUnlikely says whether u is sure enough that inv is no invoice.
func MailUnlikely(u bayes.Use, inv sources.MailInvoice) bool {
	best, ok := u.Best(mailFeatures(inv), isSenderFeature)
	return ok && best.Label == string(MailNoInvoice)
}

// isSenderFeature says whether f is the sender's address or domain: the
// evidence of a guess.
func isSenderFeature(f string) bool {
	return strings.HasPrefix(f, addrFeature) || strings.HasPrefix(f, domainFeature)
}

// mailID names inv's example: the mail's key, so a mailbox that
// renumbers its mails does not mix two of them.
func mailID(inv sources.MailInvoice) string { return inv.Key() }

// mailFeatures are inv's sender address and domain, and the words of
// its sender name, subject and attachment names (without extension:
// "Rechnung_R12.pdf" → no "pdf" in every mail).
func mailFeatures(inv sources.MailInvoice) []string {
	var out []string
	if a := strings.ToLower(strings.TrimSpace(inv.Addr)); a != "" {
		out = append(out, addrFeature+a)
	}
	if d := strings.ToLower(strings.TrimSpace(inv.Domain)); d != "" {
		out = append(out, domainFeature+d)
	}

	texts := []string{inv.Sender, inv.Subject}
	for _, a := range inv.Attachments {
		texts = append(texts, strings.TrimSuffix(a, path.Ext(a)))
	}
	for _, w := range words(texts) {
		out = append(out, wordFeature+w)
	}
	return out
}
