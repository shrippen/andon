package receipts_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/services/connections"
	"andon/internal/services/receipts"
	"andon/internal/services/svcdata"
	"andon/internal/testkit"
)

// A Paperless without the custom fields API (older than 1.17) keeps no
// receipt amounts: the receipts page says so instead of a fetch error.
func TestReceiptsNeedPaperlessFields(t *testing.T) {
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/custom_fields/" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"count": 0, "results": []}`))
	}))
	defer old.Close()

	d := testkit.DB(t)
	who, space := testkit.User(t, d, "a@b.c", enums.RoleUser)
	testkit.Conn(t, d, who, space, enums.ServiceInvoiceNinja, "https://n.example")
	docs := testkit.Conn(t, d, who, space, enums.ServicePaperless, old.URL)
	ctx := context.Background()

	// Not fetched yet: nothing known, nothing refused.
	if _, err := receipts.CountsOf(ctx, d, who, "match", time.Now().Year()); errors.Is(err, receipts.ErrPaperlessFields) {
		t.Fatal("refused before the first fetch")
	}

	conn, _ := connections.ByID(d, docs)
	if _, err := svcdata.Get(ctx, d, "paperless.data", nil, conn, model.UserHolder(who.UserID), svcdata.Force); err != nil {
		t.Fatal(err)
	}
	if _, err := receipts.CountsOf(ctx, d, who, "match", time.Now().Year()); !errors.Is(err, receipts.ErrPaperlessFields) {
		t.Fatalf("old paperless: %v", err)
	}
}
