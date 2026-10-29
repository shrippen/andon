package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"andon/internal/enums"
	"andon/internal/metrics"
	"andon/internal/services/vatcheck"
	"andon/internal/sources"
)

// TestVATDeskSplit: a desk whose proposal is a split carries its slots,
// as nothing asks /vat/split for them; any other proposal leaves the
// slots to that fetch.
func TestVATDeskSplit(t *testing.T) {
	c := vatcheck.Case{
		BankCase: metrics.BankCase{Txn: sources.NinjaBankTxn{Key: "t1", Date: "2026-10-01", Amount: 120, Debit: true}},
		Split:    []vatcheck.SplitPart{{Amount: 70, Order: true}, {Amount: 50, Order: true}, {}},
	}
	desk := func(act metrics.BankAction) string {
		c.Action = act
		rec := httptest.NewRecorder()
		values := map[string]any{"ThemeURL": "", "Partial": true, "C": c, "P": vatcheck.Page{}}
		if err := (Deps{}).Page(rec, Ctx{Locale: enums.LocaleDE}, "vat_desk", http.StatusOK, values); err != nil {
			t.Fatalf("render %s: %v", act, err)
		}
		return rec.Body.String()
	}

	if body := desk(vatcheck.ActionSplit); !strings.Contains(body, `name="s0.use_t1"`) || strings.Contains(body, `hx-get="/vat/split`) {
		t.Fatalf("a proposed split lacks its slots:\n%s", body)
	}
	if body := desk(metrics.ActionCreate); strings.Contains(body, `name="s0.use_t1"`) || !strings.Contains(body, `hx-get="/vat/split?case=t1`) {
		t.Fatalf("a proposed expense carries split slots:\n%s", body)
	}
}
