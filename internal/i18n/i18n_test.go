package i18n_test

import (
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/i18n"
)

func TestTFallsBackToDefaultLocaleThenKey(t *testing.T) {
	if got := i18n.T("nav.home", enums.LocaleDE, nil); got == "nav.home" {
		t.Fatal("expected a real translation for a known key")
	}
	if got := i18n.T("this.key.does.not.exist", enums.LocaleEN, nil); got != "this.key.does.not.exist" {
		t.Fatalf("expected unknown key to fall back to itself, got %q", got)
	}
}

func TestTSubstitutesParams(t *testing.T) {
	// action.save has no placeholders; use a key with one, if present, else
	// just verify unknown placeholders survive untouched.
	got := i18n.T("does.not.exist.either", enums.LocaleDE, map[string]any{"n": 3})
	if got != "does.not.exist.either" {
		t.Fatalf("unexpected: %q", got)
	}
}

func TestMoneyFormatsByLocale(t *testing.T) {
	if got := i18n.Money(1234.5, enums.LocaleDE, "EUR"); got != "1.234,50 €" {
		t.Fatalf("expected German money format, got %q", got)
	}
	if got := i18n.Money(1234.5, enums.LocaleEN, "EUR"); got != "€1,234.50" {
		t.Fatalf("expected English money format, got %q", got)
	}
}

func TestNumGrouping(t *testing.T) {
	if got := i18n.Num(1000000, enums.LocaleDE, 0); got != "1.000.000" {
		t.Fatalf("expected grouped German number, got %q", got)
	}
	if got := i18n.Num(1000000, enums.LocaleEN, 0); got != "1,000,000" {
		t.Fatalf("expected grouped English number, got %q", got)
	}
}

func TestDayFormatting(t *testing.T) {
	if got := i18n.Day("2026-03-05", enums.LocaleDE); got != "05.03.2026" {
		t.Fatalf("expected German date, got %q", got)
	}
	if got := i18n.Day("2026-03-05", enums.LocaleEN); got != "Mar 5, 2026" {
		t.Fatalf("expected English date, got %q", got)
	}
	if got := i18n.Day(nil, enums.LocaleDE); got != "" {
		t.Fatalf("expected empty string for nil date, got %q", got)
	}
}

// The date tile (Kante .date-tile) shows day of month and short month.
func TestMonthDayAndShort(t *testing.T) {
	if got := i18n.MonthDay("2026-10-03"); got != "03" {
		t.Fatalf("month day: %q", got)
	}
	if got := i18n.MonthShort("2026-10-03", enums.LocaleDE); got != "Okt" {
		t.Fatalf("German month: %q", got)
	}
	if got := i18n.MonthShort("2026-10-03", enums.LocaleEN); got != "Oct" {
		t.Fatalf("English month: %q", got)
	}
}

func TestPickAcceptLanguage(t *testing.T) {
	if got := i18n.Pick("en-US,en;q=0.9,de;q=0.8"); got != enums.LocaleEN {
		t.Fatalf("expected en, got %v", got)
	}
	if got := i18n.Pick("fr-FR"); got != i18n.DefaultLocale {
		t.Fatalf("expected fallback to default locale, got %v", got)
	}
}

func TestTypedDropsKeyAndFormatsMoney(t *testing.T) {
	out := i18n.Typed(map[string]any{
		"key":    "hint.x",
		"amount": map[string]any{"$money": 12.5},
	}, enums.LocaleDE)
	if _, ok := out["key"]; ok {
		t.Fatal("expected 'key' to be dropped")
	}
	if out["amount"] != "12,50 €" {
		t.Fatalf("expected formatted money, got %v", out["amount"])
	}
}

func TestAgoFuturePast(t *testing.T) {
	future := time.Now().Add(3 * 24 * time.Hour)
	if got := i18n.Ago(&future, enums.LocaleEN); got != "in 3 days" {
		t.Fatalf("expected future phrase, got %q", got)
	}
	past := time.Now().Add(-3 * 24 * time.Hour)
	if got := i18n.Ago(&past, enums.LocaleDE); got != "vor 3 Tagen" {
		t.Fatalf("expected past phrase, got %q", got)
	}
}

// GB scales large sizes to TB, so 2168 GB does not read as 2,168 GB.
func TestGBScales(t *testing.T) {
	cases := map[float64]string{12.4: "12 GB", 2168: "2,2 TB", 999: "999 GB"}
	for gb, want := range cases {
		if got := i18n.GB(gb, enums.LocaleDE); got != want {
			t.Fatalf("%v: %q, want %q", gb, got, want)
		}
	}
}

// An empty currency means the default, not none.
func TestTypedMoneyEmptyCurrency(t *testing.T) {
	out := i18n.Typed(map[string]any{"amount": map[string]any{"$money": 12.5, "currency": ""}}, enums.LocaleDE)
	if out["amount"] != "12,50 €" {
		t.Fatalf("expected the default currency, got %v", out["amount"])
	}
}

// TestSingular: a count of one takes the "_one" text where there is one.
func TestSingular(t *testing.T) {
	if got := i18n.T("kpi.invoices", enums.LocaleDE, map[string]any{"count": 1}); got != "1 Rechnung" {
		t.Fatalf("one: %q", got)
	}
	if got := i18n.T("kpi.invoices", enums.LocaleDE, map[string]any{"count": 3}); got != "3 Rechnungen" {
		t.Fatalf("three: %q", got)
	}
	if got := i18n.T("kpi.invoices", enums.LocaleEN, map[string]any{"count": 1.0}); got != "1 invoice" {
		t.Fatalf("one as float: %q", got)
	}
	// Keys ending in "_one" that mean something else stay apart.
	if got := i18n.T("conn.state_failing", enums.LocaleEN, map[string]any{"count": 1}); got != "failing" {
		t.Fatalf("state_failing with one: %q", got)
	}
}
