package analysis_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/model"
	"andon/internal/repos/content"
	"andon/internal/repos/users"
	"andon/internal/services/analysis"
	"andon/internal/services/selfbackup"
)

// TestBackupHintForAdmins: a last copy older than 26 hours becomes a hint
// in each admin's own space; a fresh copy resolves it.
func TestBackupHintForAdmins(t *testing.T) {
	d := openTestDB(t)
	admin := &model.User{Email: "a@b.c", Name: "a", Role: enums.RoleAdmin, IsActive: true, Locale: enums.LocaleDE, ColorMode: enums.ColorAuto, CreatedAt: time.Now().UTC()}
	if err := users.Add(d, admin); err != nil {
		t.Fatal(err)
	}
	if err := content.AddSpace(d, &model.Space{Kind: enums.SpacePersonal, Name: "a", OwnerUserID: &admin.ID, Version: 1}); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "backups")
	now := time.Now().UTC()
	if _, err := selfbackup.Run(d, dir, now.Add(-30*time.Hour)); err != nil {
		t.Fatal(err)
	}

	open := func() int {
		var n int
		if err := d.QueryRow("SELECT COUNT(*) FROM hints WHERE rule = 'system.backup' AND resolved_at IS NULL AND user_id = ?", admin.ID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if _, err := analysis.RunAll(context.Background(), d, now); err != nil {
		t.Fatal(err)
	}
	if open() != 1 {
		t.Fatalf("expected a backup hint, got %d", open())
	}
	if _, err := selfbackup.Run(d, dir, now); err != nil {
		t.Fatal(err)
	}
	if _, err := analysis.RunAll(context.Background(), d, now); err != nil {
		t.Fatal(err)
	}
	if open() != 0 {
		t.Fatal("fresh copy did not resolve the hint")
	}
}
