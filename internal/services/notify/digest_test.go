package notify_test

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"andon/internal/db"
	"andon/internal/enums"
	"andon/internal/outbound"
	"andon/internal/repos/content"
	"andon/internal/rules"
	"andon/internal/services/hints"
	"andon/internal/services/mail"
	"andon/internal/services/notify"
	"andon/internal/settings"
)

// TestDigestSendsOncePerDayOnChosenWeekday: the digest goes out after the
// chosen time on the chosen weekday, lists tax deadlines, and only once a day.
func TestDigestSendsOncePerDayOnChosenWeekday(t *testing.T) {
	d := openTestDB(t)
	mail.Init(settings.Settings{Testing: true, BaseURL: "http://dash.test"})
	outbound.TakeOutbox()
	userID := addUser(t, d)
	who := principalFor(t, d, userID)

	if err := notify.SaveDigest(d, who, notify.Digest{Daily: "07:30", Weekly: "mon"}); err != nil {
		t.Fatalf("save prefs: %v", err)
	}
	err := db.WithTx(d, func(tx *sql.Tx) error {
		for id := range who.Spaces {
			sp, err := content.Space(tx, id)
			if err != nil {
				return err
			}
			tax := map[string]any{"tax": map[string]any{"vat": map[string]any{"return_interval": "monthly"}}}
			return content.UpdateSpaceSettings(tx, id, tax, sp.Version)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("tax settings: %v", err)
	}

	berlin, _ := time.LoadLocation("Europe/Berlin")
	mondayEarly := time.Date(2026, 9, 28, 7, 0, 0, 0, berlin)
	tuesday := time.Date(2026, 9, 29, 8, 0, 0, 0, berlin)
	mondayLate := time.Date(2026, 9, 28, 8, 0, 0, 0, berlin)

	for _, now := range []time.Time{mondayEarly, tuesday} {
		if n, err := notify.Digests(d, now); err != nil || n != 0 {
			t.Fatalf("expected no digest at %s, got %d err=%v", now, n, err)
		}
	}
	if n, err := notify.Digests(d, mondayLate); err != nil || n != 1 {
		t.Fatalf("expected one digest, got %d err=%v", n, err)
	}
	sent := outbound.TakeOutbox()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "USt-Voranmeldung") {
		t.Fatalf("expected digest with VAT deadline, got %+v", sent)
	}
	if n, _ := notify.Digests(d, mondayLate.Add(time.Hour)); n != 0 {
		t.Fatal("expected only one digest per day")
	}
}

// TestDigestContent: the level cuts lower hints, deadlines can be left
// out, an empty digest can stay home; each run lands in the log.
func TestDigestContent(t *testing.T) {
	d := openTestDB(t)
	mail.Init(settings.Settings{Testing: true, BaseURL: "http://dash.test"})
	outbound.TakeOutbox()
	who := principalFor(t, d, addUser(t, d))
	if _, err := hints.Sync(d, onlySpace(t, who), nil, nil, []string{"kimai.missing_day"}, []rules.Finding{{
		Fingerprint: "missing:1", Rule: "kimai.missing_day", Severity: enums.SeverityInfo,
		Message: "kimai.missing_day", Params: map[string]any{"day": map[string]any{"$day": "2026-03-01"}},
	}}); err != nil {
		t.Fatalf("sync hint: %v", err)
	}

	berlin, _ := time.LoadLocation("Europe/Berlin")
	day := time.Date(2026, 9, 28, 8, 0, 0, 0, berlin)
	save := func(g notify.Digest) {
		if err := notify.SaveDigest(d, who, g); err != nil {
			t.Fatalf("save digest: %v", err)
		}
	}

	save(notify.Digest{Daily: "07:30", MinLevel: enums.SeverityWarn, Deadlines: notify.DeadlinesOff, Empty: notify.EmptySkip})
	if n, err := notify.Digests(d, day); err != nil || n != 0 {
		t.Fatalf("empty digest sent: n=%d err=%v", n, err)
	}
	if sent := outbound.TakeOutbox(); len(sent) != 0 {
		t.Fatalf("expected no mail, got %d", len(sent))
	}

	save(notify.Digest{Daily: "07:30", MinLevel: enums.SeverityInfo, Deadlines: notify.DeadlinesOff, Empty: notify.EmptySkip})
	if n, err := notify.Digests(d, day.AddDate(0, 0, 1)); err != nil || n != 1 {
		t.Fatalf("digest not sent: n=%d err=%v", n, err)
	}
	if sent := outbound.TakeOutbox(); len(sent) != 1 || !strings.Contains(sent[0].Subject, "1") {
		t.Fatalf("expected one digest with the hint, got %+v", sent)
	}

	g, err := notify.GetDigest(d, who)
	if err != nil {
		t.Fatalf("get digest: %v", err)
	}
	if len(g.Log) != 2 || g.Log[0].State != notify.DigestSent || g.Log[0].Rows != 1 || g.Log[1].State != notify.DigestEmpty {
		t.Fatalf("log %+v", g.Log)
	}
}

// TestDigestPreviewAndTest: the preview renders without sending; a test
// mail goes to the user's own address now and is logged as a test.
func TestDigestPreviewAndTest(t *testing.T) {
	d := openTestDB(t)
	mail.Init(settings.Settings{Testing: true, BaseURL: "http://dash.test"})
	outbound.TakeOutbox()
	who := principalFor(t, d, addUser(t, d))

	m, err := notify.DigestPreview(d, who)
	if err != nil || !strings.Contains(m.HTML, "<html") {
		t.Fatalf("preview: %v %q", err, m.Subject)
	}
	if sent := outbound.TakeOutbox(); len(sent) != 0 {
		t.Fatal("preview must not send")
	}

	if err := notify.DigestTest(d, who); err != nil {
		t.Fatalf("test mail: %v", err)
	}
	if sent := outbound.TakeOutbox(); len(sent) != 1 || sent[0].To != "a@x.de" {
		t.Fatalf("test mail %+v", sent)
	}
	g, _ := notify.GetDigest(d, who)
	if len(g.Log) != 1 || g.Log[0].Kind != notify.DigestByTest {
		t.Fatalf("log %+v", g.Log)
	}
}

// TestDigestTestNeedsMail: without SMTP the test says so.
func TestDigestTestNeedsMail(t *testing.T) {
	d := openTestDB(t)
	mail.Init(settings.Settings{BaseURL: "http://dash.test"})
	who := principalFor(t, d, addUser(t, d))
	if err := notify.DigestTest(d, who); err != notify.ErrNoMail {
		t.Fatalf("got %v, want ErrNoMail", err)
	}
}
