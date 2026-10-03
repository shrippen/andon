package demoworld

import (
	"encoding/json"
	"testing"
	"time"
)

// Texts, references, relative times and snake_case keys resolve as
// Decode promises.
func TestDecode(t *testing.T) {
	saved := generic
	defer func() { generic = saved }()
	if err := json.Unmarshal([]byte(`{
		"vendors": [{"id": "nordhost", "name": "Nordhost", "monthly": 29.5}],
		"it": {"thing": {"title": {"de": "Rechnung {{vendors.nordhost.name}}", "en": "Invoice"},
			"amount": "{{vendors.nordhost.monthly}}", "seen": "@-1d3h", "due": "@date+2", "call": "@date-1 09:05", "cert_days": 9}}
	}`), &generic); err != nil {
		t.Fatal(err)
	}

	var got struct {
		Title    string
		Amount   float64
		Seen     time.Time
		Due      string
		Call     time.Time
		CertDays int
	}
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	if err := Decode("it.thing", now, &got); err != nil {
		t.Fatal(err)
	}
	if got.Title != "Rechnung Nordhost" || got.Amount != 29.5 || got.Due != "2026-10-05" || got.CertDays != 9 ||
		!got.Seen.Equal(now.Add(-27*time.Hour)) ||
		!got.Call.Equal(time.Date(2026, 10, 2, 9, 5, 0, 0, time.UTC)) {
		t.Fatalf("%+v", got)
	}
}
