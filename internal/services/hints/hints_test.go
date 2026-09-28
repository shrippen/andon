package hints_test

import (
	"path/filepath"
	"testing"
	"time"

	"andon/internal/crypto"
	"andon/internal/db"
	"andon/internal/db/dbtest"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/repos/data"
	"andon/internal/rules"
	"andon/internal/services/hints"
)

func openTestDB(t *testing.T) db.Queryer {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "test.db"), dbtest.Key)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func spaceID(t *testing.T, q db.Queryer) int64 {
	t.Helper()
	sp := &model.Space{Kind: enums.SpacePersonal, Name: "x", Version: 1}
	if err := content.AddSpace(q, sp); err != nil {
		t.Fatalf("add space: %v", err)
	}
	return sp.ID
}

// backdate moves a hint's first sighting into the past.
func backdate(t *testing.T, q db.Queryer, id int64, days int) {
	t.Helper()
	at := db.TimeStr(time.Now().UTC().AddDate(0, 0, -days))
	if _, err := q.Exec("UPDATE hints SET first_seen = ? WHERE id = ?", at, id); err != nil {
		t.Fatal(err)
	}
}

func finding(fp string) rules.Finding {
	return rules.Finding{
		Fingerprint: fp, Rule: "kimai.missing_day", Severity: enums.SeverityInfo, Message: "kimai.missing_day",
		Params: map[string]any{"day": map[string]any{"$day": "2026-03-01"}},
	}
}

func TestSyncCreatesNewHint(t *testing.T) {
	q := openTestDB(t)
	sid := spaceID(t, q)

	n, err := hints.Sync(q, sid, nil, nil, []string{"kimai.missing_day"}, []rules.Finding{finding("missing:2026-03-01")})
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 fresh hint, got %d", n)
	}
}

func TestSyncResolvesGoneFindings(t *testing.T) {
	q := openTestDB(t)
	sid := spaceID(t, q)

	f := finding("missing:2026-03-01")
	if _, err := hints.Sync(q, sid, nil, nil, []string{"kimai.missing_day"}, []rules.Finding{f}); err != nil {
		t.Fatalf("first sync: %v", err)
	}
	// Second run without that finding: it should resolve.
	n, err := hints.Sync(q, sid, nil, nil, []string{"kimai.missing_day"}, nil)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 fresh hints on the resolving run, got %d", n)
	}

	stillOpen, err := dataHintsOfScope(q, sid)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(stillOpen) != 0 {
		t.Fatalf("expected the hint to be resolved (not open), got %d open", len(stillOpen))
	}
}

func TestSyncReopensResolvedHint(t *testing.T) {
	q := openTestDB(t)
	sid := spaceID(t, q)
	f := finding("missing:2026-03-01")

	if _, err := hints.Sync(q, sid, nil, nil, []string{"kimai.missing_day"}, []rules.Finding{f}); err != nil {
		t.Fatalf("sync 1: %v", err)
	}
	if _, err := hints.Sync(q, sid, nil, nil, []string{"kimai.missing_day"}, nil); err != nil {
		t.Fatalf("sync 2 (resolve): %v", err)
	}
	n, err := hints.Sync(q, sid, nil, nil, []string{"kimai.missing_day"}, []rules.Finding{f})
	if err != nil {
		t.Fatalf("sync 3 (reopen): %v", err)
	}
	if n != 1 {
		t.Fatalf("expected the reappearing finding to count as fresh (reopened), got %d", n)
	}
}

// TestReopenRestartsFirstSeen: a hint that comes back counts its age
// ("since …", escalation) from its return, not from its first sighting.
func TestReopenRestartsFirstSeen(t *testing.T) {
	q := openTestDB(t)
	sid := spaceID(t, q)
	f := finding("missing:2026-03-01")
	ids := []string{"kimai.missing_day"}

	if _, err := hints.Sync(q, sid, nil, nil, ids, []rules.Finding{f}); err != nil {
		t.Fatal(err)
	}
	h, _ := data.HintByPrint(q, sid, nil, "missing:2026-03-01")
	backdate(t, q, h.ID, 30)
	if _, err := hints.Sync(q, sid, nil, nil, ids, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := hints.Sync(q, sid, nil, nil, ids, []rules.Finding{f}); err != nil {
		t.Fatal(err)
	}
	h, _ = data.HintByPrint(q, sid, nil, "missing:2026-03-01")
	if time.Since(h.FirstSeen) > time.Hour {
		t.Fatalf("first seen kept %v after reopening", h.FirstSeen)
	}
}

func addConnection(t *testing.T, q db.Queryer, spaceID int64, key string) int64 {
	t.Helper()
	c := &model.Connection{
		SpaceID: spaceID, Key: key, Name: key, Service: "kimai", URL: "https://x",
		CredentialMode: enums.CredentialShared, VerifyTLS: true, CreatedAt: time.Now().UTC(),
	}
	if err := content.AddConnection(q, c); err != nil {
		t.Fatalf("add connection: %v", err)
	}
	return c.ID
}

func TestSyncFingerprintsPerConnection(t *testing.T) {
	q := openTestDB(t)
	sid := spaceID(t, q)
	connA := addConnection(t, q, sid, "a")
	connB := addConnection(t, q, sid, "b")
	f := finding("dup")

	nA, err := hints.Sync(q, sid, nil, &connA, []string{"kimai.missing_day"}, []rules.Finding{f})
	if err != nil {
		t.Fatalf("sync conn A: %v", err)
	}
	nB, err := hints.Sync(q, sid, nil, &connB, []string{"kimai.missing_day"}, []rules.Finding{f})
	if err != nil {
		t.Fatalf("sync conn B: %v", err)
	}
	if nA != 1 || nB != 1 {
		t.Fatalf("expected both connections to get their own hint, got %d and %d", nA, nB)
	}
}

// dataHintsOfScope is a tiny local helper avoiding an import cycle with the
// data repo's HintsOfScope for the "still open" assertion above.
func dataHintsOfScope(q db.Queryer, spaceID int64) ([]int64, error) {
	rows, err := q.Query("SELECT id FROM hints WHERE space_id = ? AND resolved_at IS NULL", spaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// TestSyncEscalatesOldHints: with escalation set, a hint open longer
// than the given days becomes critical, once noted in its history.
func TestSyncEscalatesOldHints(t *testing.T) {
	q := openTestDB(t)
	sid := spaceID(t, q)
	f := finding("missing:2026-03-01")
	f.EscalateDays = 3

	if _, err := hints.Sync(q, sid, nil, nil, []string{"kimai.missing_day"}, []rules.Finding{f}); err != nil {
		t.Fatal(err)
	}
	h, _ := data.HintByPrint(q, sid, nil, "missing:2026-03-01")
	if h.Severity != enums.SeverityInfo {
		t.Fatalf("fresh hint escalated: %v", h.Severity)
	}

	backdate(t, q, h.ID, 4)
	for range 2 {
		if _, err := hints.Sync(q, sid, nil, nil, []string{"kimai.missing_day"}, []rules.Finding{f}); err != nil {
			t.Fatal(err)
		}
	}
	h, _ = data.HintByPrint(q, sid, nil, "missing:2026-03-01")
	events, _ := data.HintEvents(q, h.ID, 10)
	escalated := 0
	for _, e := range events {
		if e.Kind == enums.EventEscalated {
			escalated++
		}
	}
	if h.Severity != enums.SeverityCritical || escalated != 1 {
		t.Fatalf("severity %v, escalation events %d", h.Severity, escalated)
	}
}

// TestResolvedLists: hints whose condition went away show in the done
// list of the last days, newest first; open ones do not.
func TestResolvedLists(t *testing.T) {
	d, err := db.Open(filepath.Join(t.TempDir(), "done.db"), dbtest.Key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	crypto.Init("test-master-key")
	who := person(t, d, "a@x.de")
	sid := ownSpace(who)
	ids := []string{"kimai.missing_day"}
	if _, err := hints.Sync(d, sid, nil, nil, ids, []rules.Finding{finding("gone"), finding("open")}); err != nil {
		t.Fatal(err)
	}
	if _, err := hints.Sync(d, sid, nil, nil, ids, []rules.Finding{finding("open")}); err != nil {
		t.Fatal(err)
	}
	done, err := hints.Resolved(d, who, time.Now().Add(-time.Hour))
	if err != nil || len(done) != 1 || done[0].ResolvedAt == nil {
		t.Fatalf("done: %+v %v", done, err)
	}
}

// Snooze choices count days from now: tomorrow, next Monday, the 1st of
// next month (local calendar days).
func TestSnoozeDays(t *testing.T) {
	wed := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC) // a Wednesday
	for choice, want := range map[string]int{"tomorrow": 1, "monday": 5, "month": 1, "week": 7, "": 0} {
		if got := hints.SnoozeDays(choice, wed); got != want {
			t.Errorf("%q: %d, want %d", choice, got, want)
		}
	}
	mon := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	if got := hints.SnoozeDays("monday", mon); got != 7 {
		t.Errorf("monday on a Monday: %d", got)
	}
}
