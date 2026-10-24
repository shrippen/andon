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

const categoriesMigration = "0026_categories.sql"

// Rule ids move to their category: hints keep their row (notes, history),
// settings move; with Sure and Firefly both set, Sure's wins.
func TestCategoriesMovesRules(t *testing.T) {
	d, err := sql.Open(driverName, "file:"+filepath.Join(t.TempDir(), "m.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	migrateBefore(t, d, categoriesMigration)

	settings := `{"rules": {"firefly.low_balance": {"min_amount": 50}, "sure.low_balance": {"min_amount": 300},
		"snipe.warranty_expiring": {"info_days": 30}, "snipe.license_seats": {"enabled": false}}}`
	seed := []string{
		`INSERT INTO spaces (id, kind, name, settings) VALUES (1, 'instance', 'i', '` + settings + `')`,
		`INSERT INTO hints (id, space_id, fingerprint, rule, severity, message, first_seen, last_seen)
		 VALUES (5, 1, '3:warranty:12', 'snipe.warranty_expiring', 10, 'snipe.warranty', '', '')`,
		`INSERT INTO hints (id, space_id, fingerprint, rule, severity, message, first_seen, last_seen)
		 VALUES (6, 1, '4:low:a1', 'firefly.low_balance', 20, 'sure.low', '', '')`,
		`INSERT INTO hint_events (hint_id, kind, note, at) VALUES (5, 'note', 'Händler angerufen', '')`,
	}
	if _, err := d.Exec(strings.Join(seed, ";\n")); err != nil {
		t.Fatalf("seed: %v", err)
	}

	applyMigration(t, d, categoriesMigration)

	rows := map[int]string{}
	for _, id := range []int{5, 6} {
		var rule, msg string
		if err := d.QueryRow(`SELECT rule, message FROM hints WHERE id = ?`, id).Scan(&rule, &msg); err != nil {
			t.Fatal(err)
		}
		rows[id] = rule + " " + msg
	}
	if rows[5] != "inventory.warranty_expiring inventory.warranty" || rows[6] != "bank.low_balance bank.low" {
		t.Errorf("hints = %v", rows)
	}
	var notes int
	if err := d.QueryRow(`SELECT COUNT(*) FROM hint_events WHERE hint_id = 5`).Scan(&notes); err != nil || notes != 1 {
		t.Errorf("notes = %d, %v", notes, err)
	}

	var got string
	if err := d.QueryRow(`SELECT json_extract(settings, '$.rules') FROM spaces WHERE id = 1`).Scan(&got); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"bank.low_balance":{"min_amount":300}`, `"inventory.warranty_expiring":{"info_days":30}`, `"snipe.license_seats":{"enabled":false}`} {
		if !strings.Contains(got, want) {
			t.Errorf("settings %s lack %s", got, want)
		}
	}
	for _, gone := range []string{"firefly.low_balance", "sure.low_balance", "snipe.warranty_expiring"} {
		if strings.Contains(got, gone) {
			t.Errorf("settings %s keep %s", got, gone)
		}
	}
}
