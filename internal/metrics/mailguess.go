package metrics

import (
	"path"
	"slices"
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
//	  w:nordhost w:ihre …         words    Suggest → invoice / no invoice
//
// The keyword filter of the source still picks the candidates; the model
// only suggests, the user decides. Only the sender is
// evidence: words ("ihre", "your") are shared by invoices and others
// alike, they only weigh in.

// MailKind is what the user decided a mail is: the model's label.
type MailKind string

const (
	MailNoKind    MailKind = "" // no suggestion
	MailIsInvoice MailKind = "invoice"
	MailNoInvoice MailKind = "other"
)

// Feature prefixes of a mail: its sender's address and domain; words
// share wordFeature with the EÜR model.
const (
	addrFeature   = "f:"
	domainFeature = "d:"
)

// MailOpen is box without the mails dismissed holds set aside as no
// invoice: rules, receipts and tiles then count invoices only. box itself
// stays as it is (it is the cache's).
func MailOpen(box *sources.MailDataset, dismissed func(sources.MailInvoice) bool) *sources.MailDataset {
	if box == nil || !slices.ContainsFunc(box.Invoices, dismissed) {
		return box
	}
	out := *box
	out.Invoices = slices.DeleteFunc(slices.Clone(box.Invoices), dismissed)
	return &out
}

// MailLearn teaches m that inv is of kind k; a mail decided again
// replaces its earlier example. It says whether m changed.
func MailLearn(m *bayes.Model, inv sources.MailInvoice, k MailKind) bool {
	return m.Learn(mailID(inv), string(k), mailFeatures(inv))
}

// MailForget takes back what m learned from inv (a decision undone).
func MailForget(m *bayes.Model, inv sources.MailInvoice) bool {
	return m.Forget(mailID(inv))
}

// MailSuggest is what u suggests inv is; MailNoKind without a sure
// enough guess. Two kinds only: the likelier one says it all.
func MailSuggest(u bayes.Use, inv sources.MailInvoice) MailKind {
	picks := u.Suggest(mailFeatures(inv), isSenderFeature, 1)
	if len(picks) == 0 {
		return MailNoKind
	}
	return MailKind(picks[0].Label)
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
