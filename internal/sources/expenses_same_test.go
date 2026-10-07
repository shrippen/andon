package sources_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"andon/internal/sources"
)

// The receipts page searches the expenses the tax export lists: both
// reads ask Invoice Ninja the same way, so a deleted expense is in
// neither.
func TestExpenseReadsAskAlike(t *testing.T) {
	var mu sync.Mutex
	asked := map[string]string{} // source → is_deleted of its expenses read
	empty := []byte(`{"data": [], "meta": {"pagination": {"total_pages": 1}}}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/expenses" {
			who := "data"
			if r.URL.Query().Get("include") == "vendor" {
				who = "receipts"
			}
			mu.Lock()
			asked[who] = r.URL.Query().Get("is_deleted")
			mu.Unlock()
		}
		w.Write(empty)
	}))
	defer srv.Close()

	sctx := sources.Ctx{URL: srv.URL, Secret: "tok", VerifyTLS: true}
	for _, src := range []interface {
		Fetch(context.Context, sources.Ctx) (any, error)
	}{sources.NinjaData, sources.NinjaExpenses} {
		if _, err := src.Fetch(context.Background(), sctx); err != nil {
			t.Fatalf("fetch: %v", err)
		}
	}
	if asked["data"] != "false" || asked["receipts"] != "false" {
		t.Fatalf("expense reads differ: %v", asked)
	}
}

// The demo's receipts page and its tax export know the same expenses.
func TestDemoExpensesLikeExport(t *testing.T) {
	now := time.Now()
	key := func(day string, amount float64) string { return day + " " + strconv.FormatFloat(amount, 'f', 2, 64) }
	var export, search []string
	for _, e := range sources.DemoNinja(now).Expenses {
		export = append(export, key(e.Date, e.Amount))
	}
	for _, e := range sources.DemoExpenses(now).Expenses {
		search = append(search, key(e.Day, e.Amount))
	}
	slices.Sort(export)
	slices.Sort(search)
	if !slices.Equal(export, search) {
		t.Fatalf("export %v\nsearch %v", export, search)
	}
}
