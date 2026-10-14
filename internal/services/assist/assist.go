// Package assist asks Claude for help, only when a user clicks for it:
//
//	Advise       "Was tun?" for one hint: its title, reason, rule and
//	             services go out, never location hints; kept per language
//	             until the hint text changes
//	ReadInvoice  invoice fields from a mail's PDF or image attachments
//	Classify     open bank bookings: private or business, vendor,
//	             category and tax rate; payee, text, amount and Sure's
//	             names go out, nothing else
package assist

import (
	"andon/internal/outbound"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/i18n"
	data "andon/internal/repos/data"
	"andon/internal/services/access"
	"andon/internal/services/hints"
	"andon/internal/settings"
)

// locationRules never leave the instance (where a user was).
const locationRules = "geo."

var (
	// ErrOff means the instance has no API key.
	ErrOff = errors.New("assist.off")
	// ErrPrivate means the hint must not leave the instance.
	ErrPrivate = errors.New("assist.private")
	// ErrUnreadable means no invoice fields came back.
	ErrUnreadable = errors.New("assist.unreadable")
)

var apiKey string

// complete is the LLM call; tests swap it.
var complete = outbound.AskFiles

// Init reads the API key; without one the features stay hidden.
func Init(cfg settings.Settings) { apiKey = cfg.AnthropicAPIKey }

// Enabled reports whether the instance has an API key.
func Enabled() bool { return apiKey != "" }

// Advise returns concrete next steps for one hint.
func Advise(ctx context.Context, d *sql.DB, who *access.Principal, hintID int64) (string, error) {
	if !Enabled() {
		return "", ErrOff
	}
	hint, err := hints.One(d, who, hintID)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(hint.Rule, locationRules) {
		return "", ErrPrivate
	}

	locale := who.Locale
	prompt := fmt.Sprintf("%s: %s\n%s: %s\n%s: %s\n%s: %s\n%s: %s\n",
		i18n.T("assist.services", locale, nil), strings.Join(hint.Sources, ", "),
		i18n.T("assist.rule", locale, nil), i18n.T("rule_name."+hint.Rule, locale, nil),
		i18n.T("assist.level", locale, nil), i18n.T("severity."+hint.Severity.Key(), locale, nil),
		i18n.T("assist.hint", locale, nil), hint.Title,
		i18n.T("assist.why", locale, nil), hint.Why)
	sum := sha256.Sum256([]byte(prompt))
	digest := fmt.Sprintf("%x", sum[:8])

	if text, ok, err := data.Advice(d, hintID, string(locale), digest); err != nil || ok {
		return text, err
	}
	text, err := complete(ctx, apiKey, i18n.T("assist.advice_system", locale, nil), prompt, nil)
	if err != nil {
		return "", err
	}
	return text, db.WithTx(d, func(tx *sql.Tx) error { return data.SaveAdvice(tx, hintID, string(locale), digest, text) })
}

// Invoice is what an invoice says about itself; empty when unknown.
type Invoice struct {
	Vendor   string  `json:"vendor"`
	Number   string  `json:"number"`
	Date     string  `json:"date"`
	Due      string  `json:"due"`
	Net      float64 `json:"net"`
	VAT      float64 `json:"vat"`
	Gross    float64 `json:"gross"`
	Currency string  `json:"currency"`
}

// Title names an invoice for Paperless: "Hetzner R0012345".
func (i Invoice) Title() string {
	return strings.TrimSpace(i.Vendor + " " + i.Number)
}

// ReadInvoice reads invoice fields from attachments.
func ReadInvoice(ctx context.Context, files []outbound.LLMFile) (Invoice, error) {
	if !Enabled() {
		return Invoice{}, ErrOff
	}
	answer, err := complete(ctx, apiKey, invoiceSystem, invoicePrompt, files)
	if err != nil {
		return Invoice{}, err
	}
	start, end := strings.Index(answer, "{"), strings.LastIndex(answer, "}")
	if start < 0 || end < start {
		return Invoice{}, ErrUnreadable
	}
	var inv Invoice
	if err := json.Unmarshal([]byte(answer[start:end+1]), &inv); err != nil || (inv.Vendor == "" && inv.Gross == 0) {
		return Invoice{}, ErrUnreadable
	}
	return inv, nil
}

// The invoice prompt is fixed English: the answer is data, not prose.
const (
	invoiceSystem = "You read invoices. Answer with one JSON object only, no prose."
	invoicePrompt = `Read the attached invoice and return:
{"vendor": "", "number": "", "date": "YYYY-MM-DD", "due": "YYYY-MM-DD", "net": 0, "vat": 0, "gross": 0, "currency": "EUR"}
Use "" or 0 for anything the invoice does not state. Amounts as numbers with a dot.`
)

// Booking is one open bank transaction the model sorts: what the bank
// and Sure say about it, nothing else.
type Booking struct {
	Key, Date, Payer, Text     string
	Amount                     float64
	Debit                      bool
	SureMerchant, SureCategory string
	SureTags                   []string
}

// Ledger is what the model may choose from: Invoice Ninja's vendors,
// expense categories and tax rates.
type Ledger struct {
	Vendors, Categories []string
	TaxRates            []float64
}

// Guess is the model's reading of one booking. A category or rate
// outside the ledger is dropped; TaxKnown says whether a rate is left.
type Guess struct {
	Key      string  `json:"key"`
	Private  bool    `json:"private"`
	Vendor   string  `json:"vendor"`
	Category string  `json:"category"`
	TaxRate  float64 `json:"tax_rate"`
	TaxKnown bool    `json:"tax_known"`
	Why      string  `json:"why"`
}

// maxBookings caps one request: a quarter of a small business.
const maxBookings = 60

// Classify asks which bookings are private and, for business ones, the
// vendor, category and tax rate. Why comes in the user's language.
func Classify(ctx context.Context, locale enums.Locale, list []Booking, ledger Ledger) ([]Guess, error) {
	if !Enabled() {
		return nil, ErrOff
	}
	list = list[:min(len(list), maxBookings)]
	in, err := json.Marshal(map[string]any{"bookings": list, "vendors": ledger.Vendors, "categories": ledger.Categories,
		"tax_rates": ledger.TaxRates, "language": string(locale)})
	if err != nil {
		return nil, err
	}
	answer, err := complete(ctx, apiKey, classifySystem, classifyPrompt+string(in), nil)
	if err != nil {
		return nil, err
	}
	start, end := strings.Index(answer, "["), strings.LastIndex(answer, "]")
	if start < 0 || end < start {
		return nil, ErrUnreadable
	}
	var guesses []Guess
	if err := json.Unmarshal([]byte(answer[start:end+1]), &guesses); err != nil {
		return nil, ErrUnreadable
	}
	return ledger.keep(guesses, list), nil
}

// keep drops guesses for unknown bookings and choices outside the ledger.
func (l Ledger) keep(guesses []Guess, list []Booking) []Guess {
	asked := map[string]bool{}
	for _, b := range list {
		asked[b.Key] = true
	}
	var out []Guess
	for _, g := range guesses {
		if !asked[g.Key] {
			continue
		}
		g.TaxKnown = !g.Private && slices.Contains(l.TaxRates, g.TaxRate)
		if !g.TaxKnown {
			g.TaxRate = 0
		}
		if !slices.ContainsFunc(l.Categories, func(c string) bool { return strings.EqualFold(c, g.Category) }) {
			g.Category = ""
		}
		out = append(out, g)
	}
	return out
}

// The classify prompt is fixed English: the answer is data.
const (
	classifySystem = "You help a German freelancer prepare the VAT return. Answer with one JSON array only, no prose."
	classifyPrompt = `Each booking below left the business bank account ("debit": true) or came in. For each, decide:
- "private": true if it is private spending (groceries, streaming for home, private insurance), not a business expense.
- "vendor": the business name, preferring an entry of "vendors" when it is the same company.
- "category": one entry of "categories", or "" if none fits.
- "tax_rate": the German input VAT rate in percent the invoice most likely carries, one of "tax_rates"; 0 for none (insurance, fees, foreign reverse charge).
- "why": one short sentence in the language "language".
Answer: [{"key": "", "private": false, "vendor": "", "category": "", "tax_rate": 19, "why": ""}]
Input:
`
)

// Reminder is what a reminder mail is about: the facts the model may use.
type Reminder struct {
	Client, Number     string
	Amount, Balance    float64
	Currency, Due      string
	OverdueDays, Stage int  // the reminder to write (1–3)
	PaysLate           bool // the client usually pays late
}

// ReminderText is a drafted mail.
type ReminderText struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// DraftReminder writes a reminder in the user's language: friendly at
// stage 1, firmer later, never threatening; facts only from r.
func DraftReminder(ctx context.Context, locale enums.Locale, r Reminder) (ReminderText, error) {
	if !Enabled() {
		return ReminderText{}, ErrOff
	}
	facts, _ := json.Marshal(r)
	prompt := fmt.Sprintf(reminderPrompt, languageOf(locale), facts)
	answer, err := complete(ctx, apiKey, reminderSystem, prompt, nil)
	if err != nil {
		return ReminderText{}, err
	}
	start, end := strings.Index(answer, "{"), strings.LastIndex(answer, "}")
	if start < 0 || end < start {
		return ReminderText{}, ErrUnreadable
	}
	var out ReminderText
	if err := json.Unmarshal([]byte(answer[start:end+1]), &out); err != nil || out.Body == "" {
		return ReminderText{}, ErrUnreadable
	}
	return out, nil
}

// languageOf names a locale's language for the prompt.
func languageOf(locale enums.Locale) string {
	if locale == enums.LocaleDE {
		return "German"
	}
	return "English"
}

// The reminder prompt is fixed English; the mail comes in the user's
// language.
const (
	reminderSystem = "You write short, polite payment reminders for a freelancer. Answer with one JSON object only."
	reminderPrompt = `Write payment reminder number "Stage" (1 friendly, 2 clear, 3 final before further steps; never threaten) in %s for this invoice:
%s
Use only these facts. Return {"subject": "", "body": ""}; plain text body, no placeholders, no signature.`
)
