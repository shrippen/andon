package hints_test

import (
	"errors"
	"path/filepath"
	"testing"

	"andon/internal/crypto"
	"andon/internal/db"
	"andon/internal/db/dbtest"
	"andon/internal/enums"
	"andon/internal/rules"
	"andon/internal/services/hints"
	"andon/internal/services/util"
)

// A runbook belongs to a rule in a space: every hint of that rule there
// shows it. Only who may edit the space writes it; others cannot reach it.
func TestRunbookPerRule(t *testing.T) {
	crypto.Init("test-master-key")
	d, err := db.Open(filepath.Join(t.TempDir(), "r.db"), dbtest.Key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	alex, kim := person(t, d, "a@x.de"), person(t, d, "k@x.de")
	sid := ownSpace(alex)
	rule := []string{"kimai.missing_day"}
	if _, err := hints.Sync(d, sid, nil, nil, rule, []rules.Finding{finding("a"), finding("b")}); err != nil {
		t.Fatal(err)
	}
	views, _ := hints.Active(d, alex, enums.SeverityInfo, nil, 0)

	if err := hints.SetRunbook(d, alex, views[0].ID, " 1. Kimai öffnen\n2. Tag nachtragen "); err != nil {
		t.Fatal(err)
	}
	book, err := hints.Runbook(d, alex, views[1].ID)
	if err != nil || book.Text != "1. Kimai öffnen\n2. Tag nachtragen" || !book.CanEdit {
		t.Fatalf("runbook of the other hint: %+v %v", book, err)
	}
	if err := hints.SetRunbook(d, kim, views[0].ID, "x"); !errors.Is(err, hints.ErrNotFound) && !errors.Is(err, util.ErrNotFound) && !errors.Is(err, hints.ErrDenied) {
		t.Fatalf("outsider: %v", err)
	}
}
