package db

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
)

const levelsMigration = "0018_connection_levels.sql"

// migrateBefore applies every migration that sorts before stop.
func migrateBefore(t *testing.T, d *sql.DB, stop string) {
	t.Helper()
	names, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range names {
		if n.Name() >= stop {
			return
		}
		applyMigration(t, d, n.Name())
	}
}

func applyMigration(t *testing.T, d *sql.DB, name string) {
	t.Helper()
	body, err := migrationFiles.ReadFile("migrations/" + name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(string(body)); err != nil {
		t.Fatalf("migration %s: %v", name, err)
	}
}

// A personal-space template with a leftover shared grant (user 0) and
// the owner's grant: the owner's grant wins, no UNIQUE conflict.
func TestLevelsKeepsOwnerGrant(t *testing.T) {
	d, err := sql.Open(driverName, "file:"+filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	migrateBefore(t, d, levelsMigration)

	seed := []string{
		`INSERT INTO users (id, email, name, created_at) VALUES (7, 'a@x', 'a', '')`,
		`INSERT INTO spaces (id, kind, name, owner_user_id) VALUES (1, 'personal', 'p', 7)`,
		`INSERT INTO connections (id, space_id, key, name, service, url, credential_mode, created_at)
		 VALUES (3, 1, 'k', 'k', 'gitea', 'http://x', 'personal', '')`,
		`INSERT INTO oauth_grants (connection_id, user_id, grant_enc, updated_at) VALUES (3, 0, x'00', '')`,
		`INSERT INTO oauth_grants (connection_id, user_id, grant_enc, updated_at) VALUES (3, 7, x'07', '')`,
	}
	if _, err := d.Exec(strings.Join(seed, ";\n")); err != nil {
		t.Fatalf("seed: %v", err)
	}

	applyMigration(t, d, levelsMigration)

	const ownerGrant = "\x07"
	var grant []byte
	if err := d.QueryRow(`SELECT grant_enc FROM oauth_grants WHERE connection_id = 3 AND user_id = 0`).Scan(&grant); err != nil {
		t.Fatal(err)
	}
	if string(grant) != ownerGrant {
		t.Fatalf("shared grant = %x, want owner's %x", grant, ownerGrant)
	}
}
