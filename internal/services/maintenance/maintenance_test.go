package maintenance_test

import (
	"archive/tar"
	"compress/gzip"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"andon/internal/crypto"
	"andon/internal/db"
	"andon/internal/services/maintenance"
)

// oldMasterKey is the key a test database starts with.
const oldMasterKey = "test-master-key"

// openTestDB starts a new install: salt written, database open.
func openTestDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, _, err := maintenance.Unlock(path, oldMasterKey)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	return d, path
}

func TestBackupProducesReadableArchive(t *testing.T) {
	d, path := openTestDB(t)
	target := t.TempDir()

	archive, err := maintenance.Backup(d, path, target, "")
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	f, err := os.Open(archive)
	if err != nil {
		t.Fatalf("open archive: %v", err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	tr := tar.NewReader(gz)
	hdr, err := tr.Next()
	if err != nil {
		t.Fatalf("tar entry: %v", err)
	}
	if hdr.Name != "andon.db" || hdr.Size == 0 {
		t.Fatalf("unexpected archive entry: %+v", hdr)
	}
}

// newMasterKey is a strong key, as `openssl rand -base64 32` makes one.
const newMasterKey = "q1dW3V0r3a9mGx6+Yt7n2ZkQv5LbHs8PjR4uE0cXf1o="

// Rotating to a guessable key would weaken every secret; it is refused
// before anything is re-encrypted.
func TestRotateKeyRefusesWeakKey(t *testing.T) {
	d, path := openTestDB(t)
	if _, err := maintenance.RotateKey(d, path, "new-master-key"); !errors.Is(err, maintenance.ErrWeakKey) {
		t.Fatalf("weak key: %v", err)
	}
}

// After a rotation the old key still starts the old database; the new
// key swaps the copy in, and from then on only the new key opens it.
func TestRotateThenRestart(t *testing.T) {
	d, path := openTestDB(t)
	places := storeEverything(t, d)
	if _, err := maintenance.RotateKey(d, path, newMasterKey); err != nil {
		t.Fatalf("rotate: %v", err)
	}
	d.Close()

	for _, step := range []struct {
		key  string
		fail bool
	}{{oldMasterKey, false}, {newMasterKey, false}, {oldMasterKey, true}} {
		again, _, err := maintenance.Unlock(path, step.key)
		if step.fail {
			if !errors.Is(err, db.ErrKey) {
				t.Fatalf("old key after the swap: %v", err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("start with %s: %v", step.key, err)
		}
		expectReadable(t, again, places)
		again.Close()
	}
}

// expectReadable checks every stored secret opens under the process key.
func expectReadable(t *testing.T, q db.Queryer, places []sealed) {
	t.Helper()
	for _, p := range places {
		if text, err := crypto.Decrypt(p.read(q), p.purpose); err != nil || text != "secret" {
			t.Errorf("%s: %q, %v", p.name, text, err)
		}
	}
}

// The archive carries uploaded icons and theme files next to the
// database: they only exist on disk and are gone otherwise.
func TestBackupIncludesAssets(t *testing.T) {
	d, path := openTestDB(t)
	dataDir := t.TempDir()
	for _, f := range []string{"icons/ab/icon.png", "themes/7/Inter-600.woff2"} {
		p := filepath.Join(dataDir, f)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o640); err != nil {
			t.Fatal(err)
		}
	}

	archive, err := maintenance.Backup(d, path, t.TempDir(), dataDir)
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		names[hdr.Name] = true
	}
	for _, want := range []string{"andon.db", "icons/ab/icon.png", "themes/7/Inter-600.woff2"} {
		if !names[want] {
			t.Errorf("missing %s in %v", want, names)
		}
	}
}

// A backup restores on its own: unpacked anywhere, the archive's
// database and salt open with MASTER_KEY.
func TestBackupRestores(t *testing.T) {
	d, path := openTestDB(t)
	places := storeEverything(t, d)
	archive, err := maintenance.Backup(d, path, t.TempDir(), "")
	if err != nil {
		t.Fatalf("backup: %v", err)
	}

	restored := filepath.Join(t.TempDir(), "andon.db")
	f, err := os.Open(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(filepath.Dir(restored), hdr.Name), body, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	again, how, err := maintenance.Unlock(restored, oldMasterKey)
	if err != nil || how != maintenance.UnlockedAsIs {
		t.Fatalf("open restored backup: %v %v", how, err)
	}
	defer again.Close()
	expectReadable(t, again, places)
}
