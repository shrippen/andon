package receipts_test

import (
	"context"
	"errors"
	"testing"

	"andon/internal/enums"
	"andon/internal/services/access"
	"andon/internal/services/receipts"
	"andon/internal/testkit"
)

// Linking and creating write to Invoice Ninja and Paperless: using both
// connections is not enough.
func TestWritesNeedEditRight(t *testing.T) {
	d := testkit.DB(t)
	instance := testkit.Instance(t, d)
	boss, _ := testkit.User(t, d, "admin@x.de", enums.RoleAdmin)
	user, _ := testkit.User(t, d, "user@x.de", enums.RoleUser)
	testkit.Conn(t, d, boss, instance, enums.ServiceInvoiceNinja, "https://ninja.example")
	testkit.Conn(t, d, boss, instance, enums.ServicePaperless, "https://docs.example")
	ctx := context.Background()

	if _, err := receipts.Link(ctx, d, user, "exp1", []int64{1}, ""); !errors.Is(err, access.ErrDenied) {
		t.Fatalf("link with USE: %v", err)
	}
	if err := receipts.Unlink(ctx, d, user, "exp1", 1, ""); !errors.Is(err, access.ErrDenied) {
		t.Fatalf("unlink with USE: %v", err)
	}
	if _, err := receipts.Create(ctx, d, user, 1, receipts.NewExpense{Amount: 5}, ""); !errors.Is(err, access.ErrDenied) {
		t.Fatalf("create with USE: %v", err)
	}
}
