// Package ask is "Frag Andon" (experimental): a question about the
// user's own figures, answered by Claude from facts Andon collects. It
// only reads; location hints never go out.
//
//	question ─┐
//	hints ────┼─► facts (titles, customers' figures) ─► Claude ─► answer
//	customers ┘
package ask

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/services/access"
	"andon/internal/services/assist"
	"andon/internal/services/clients"
	"andon/internal/services/hints"
)

// ErrEmpty means no question was asked.
var ErrEmpty = errors.New("ask.err_empty")

// questionMax bounds a question.
const questionMax = 500

// privateRules never leave the instance (where a user was).
const privateRules = "geo."

// hintsMax is how many hints go along, most severe first.
const hintsMax = 60

// Answer asks Claude the question with the user's facts.
func Answer(ctx context.Context, d *sql.DB, who *access.Principal, question string) (string, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return "", ErrEmpty
	}
	if len(question) > questionMax {
		question = question[:questionMax]
	}
	facts, err := Facts(ctx, d, who)
	if err != nil {
		return "", err
	}
	return assist.Ask(ctx, who.Locale, question, facts)
}

// Facts are the lines sent along: open hints and customers' figures of
// the last twelve months.
func Facts(ctx context.Context, d *sql.DB, who *access.Principal) (string, error) {
	var b strings.Builder
	open, err := hints.Active(d, who, enums.SeverityInfo, nil, hintsMax)
	if err != nil {
		return "", err
	}
	b.WriteString("Open hints:\n")
	for _, h := range open {
		if strings.HasPrefix(h.Rule, privateRules) {
			continue
		}
		fmt.Fprintf(&b, "- [%s] %s\n", severityName(h.Severity), h.Title)
	}

	cards, err := clients.List(ctx, d, who, metrics.PeriodLast12)
	if err != nil {
		return "", err
	}
	b.WriteString("\nCustomers (last 12 months):\n")
	for _, c := range cards {
		fmt.Fprintf(&b, "- %s: %.1f h, revenue %.2f, unbilled %.2f, open %.2f, overdue %.2f, pays in %d days, last work %s\n",
			c.Name, c.Hours, c.Revenue, c.Unbilled, c.Open, c.Overdue, c.PaymentDays, c.LastWork)
	}
	return b.String(), nil
}

func severityName(s enums.Severity) string {
	switch {
	case s >= enums.SeverityCritical:
		return "critical"
	case s >= enums.SeverityWarn:
		return "warning"
	}
	return "info"
}
