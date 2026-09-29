package selfbackup_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"andon/internal/crypto"
	"andon/internal/db"
	"andon/internal/db/dbtest"
	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/repos/users"
	"andon/internal/services/selfbackup"
)

// TestRunVerifiesAndPrunes: each run leaves a copy that passes the
// restore test (rows, decryptable secrets); only Keep copies stay.
func TestRunVerifiesAndPrunes(t *testing.T) {
	crypto.Init(crypto.Derive("test-master-key", nil))
	live := filepath.Join(t.TempDir(), "live.db")
	if err := db.WriteSalt(live, crypto.NewSalt()); err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(live, dbtest.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	u := &model.User{Email: "a@b.c", Name: "a", Role: enums.RoleAdmin, IsActive: true, Locale: enums.LocaleDE, ColorMode: enums.ColorAuto, CreatedAt: time.Now().UTC()}
	if err := users.Add(d, u); err != nil {
		t.Fatal(err)
	}
	space := &model.Space{Kind: enums.SpacePersonal, Name: "a", OwnerUserID: &u.ID, Version: 1}
	if err := content.AddSpace(d, space); err != nil {
		t.Fatal(err)
	}
	secret, _ := crypto.Encrypt("tok", crypto.PurposeCredential, nil)
	conn := &model.Connection{SpaceID: space.ID, Key: "k", Name: "k", Service: "kimai", URL: "http://k", SecretEnc: secret,
		CredentialMode: enums.CredentialShared, CreatedAt: time.Now().UTC()}
	if err := content.AddConnection(d, conn); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(t.TempDir(), "backups")
	start := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)
	for i := range selfbackup.Keep + 2 {
		status, err := selfbackup.Run(d, dir, start.AddDate(0, 0, i))
		if err != nil {
			t.Fatal(err)
		}
		if !status.OK || status.Rows["users"] != 1 || status.Secrets != 1 {
			t.Fatalf("run %d: %+v", i, status)
		}
	}
	files, _ := selfbackup.Files(dir)
	if len(files) != selfbackup.Keep || files[0].Name != "andon-20260909-030000.db" {
		t.Fatalf("files: %+v", files)
	}
	// Each copy keeps the salt its key needs, and loses it with the copy.
	if salts, _ := filepath.Glob(filepath.Join(dir, "*.salt")); len(salts) != selfbackup.Keep {
		t.Fatalf("salts: %v", salts)
	}
	last, err := selfbackup.Last(d)
	if err != nil || last == nil || !last.OK || last.File != files[0].Name {
		t.Fatalf("last: %+v %v", last, err)
	}

	// A copy the master key cannot open fails the test and is dropped.
	crypto.Init(crypto.Derive("other-key", nil))
	defer crypto.Init(crypto.Derive("test-master-key", nil))
	status, _ := selfbackup.Run(d, dir, start.AddDate(0, 1, 0))
	if status.OK || status.Problem != "selfbackup.secrets" {
		t.Fatalf("wrong key passed: %+v", status)
	}
	if _, err := os.Stat(filepath.Join(dir, status.File)); !os.IsNotExist(err) {
		t.Fatalf("failed copy kept")
	}
	if _, err := os.Stat(filepath.Join(dir, status.File+".salt")); !os.IsNotExist(err) {
		t.Fatalf("failed copy's salt kept")
	}
}

// TestRunDueSurvivesRestarts: a process restarted more often than once a
// day still backs up, because the job checks the last copy's age instead
// of waiting a day after start.
func TestRunDueSurvivesRestarts(t *testing.T) {
	crypto.Init(crypto.Derive("test-master-key", nil))
	d, err := db.Open(filepath.Join(t.TempDir(), "live.db"), dbtest.Key)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	dir := filepath.Join(t.TempDir(), "backups")
	start := time.Date(2026, 9, 1, 3, 0, 0, 0, time.UTC)

	for _, step := range []struct {
		after time.Duration
		ran   bool
	}{{0, true}, {time.Hour, false}, {selfbackup.Interval + time.Minute, true}} {
		ran, err := selfbackup.RunDue(d, dir, start.Add(step.after))
		if err != nil {
			t.Fatal(err)
		}
		if ran != step.ran {
			t.Fatalf("after %v: ran=%v, want %v", step.after, ran, step.ran)
		}
	}
}
