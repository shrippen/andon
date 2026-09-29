package maintenance_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"andon/internal/crypto"
	"andon/internal/db"
	"andon/internal/services/maintenance"
)

// legacyDB writes a database as installs before salts did: file and
// secrets under SHA-256 of the key. It returns the path and what it
// stored.
func legacyDB(t *testing.T) (string, []sealed) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "andon.db")
	m := crypto.Legacy(oldMasterKey)
	crypto.Init(m)
	fileKey, err := crypto.DatabaseKey(m)
	if err != nil {
		t.Fatal(err)
	}
	d, err := db.Open(path, fileKey)
	if err != nil {
		t.Fatal(err)
	}
	places := storeEverything(t, d)
	d.Close()
	return path, places
}

// A new install gets a salt before its database exists.
func TestUnlockNewInstall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "andon.db")
	d, how, err := maintenance.Unlock(path, oldMasterKey)
	if err != nil || how != maintenance.UnlockedNew {
		t.Fatalf("unlock: %v %v", how, err)
	}
	d.Close()
	if salt, _ := db.ReadSalt(path); len(salt) == 0 {
		t.Fatal("no salt written")
	}
	if _, how, _ := maintenance.Unlock(path, oldMasterKey); how != maintenance.UnlockedAsIs {
		t.Fatalf("second start: %v", how)
	}
}

// An install from before salts moves to the Argon2id key on its first
// start: every secret still reads, the unsalted key no longer opens it.
func TestUnlockUpgradesLegacy(t *testing.T) {
	path, places := legacyDB(t)

	d, how, err := maintenance.Unlock(path, oldMasterKey)
	if err != nil || how != maintenance.UnlockedUpgraded {
		t.Fatalf("unlock: %v %v", how, err)
	}
	expectReadable(t, d, places)
	d.Close()

	legacyKey, _ := crypto.DatabaseKey(crypto.Legacy(oldMasterKey))
	if old, err := db.Open(path, legacyKey); err == nil {
		old.Close()
		t.Fatal("the unsalted key still opens the database")
	}
	again, how, err := maintenance.Unlock(path, oldMasterKey)
	if err != nil || how != maintenance.UnlockedAsIs {
		t.Fatalf("next start: %v %v", how, err)
	}
	expectReadable(t, again, places)
	again.Close()
}

// A crash during the upgrade, before the salt was written, leaves a
// stray copy; the next start ignores it and upgrades again.
func TestUnlockAfterInterruptedUpgrade(t *testing.T) {
	path, places := legacyDB(t)
	if err := os.WriteFile(path+".rekeyed", []byte("half written"), 0o600); err != nil {
		t.Fatal(err)
	}

	d, how, err := maintenance.Unlock(path, oldMasterKey)
	if err != nil || how != maintenance.UnlockedUpgraded {
		t.Fatalf("unlock: %v %v", how, err)
	}
	expectReadable(t, d, places)
	d.Close()
}

// A wrong key opens nothing and leaves the install as it was: no salt,
// so the right key still upgrades it.
func TestUnlockWrongKeyChangesNothing(t *testing.T) {
	path, _ := legacyDB(t)
	if _, _, err := maintenance.Unlock(path, newMasterKey); !errors.Is(err, db.ErrKey) {
		t.Fatalf("wrong key: %v", err)
	}
	if salt, _ := db.ReadSalt(path); salt != nil {
		t.Fatal("a wrong key wrote a salt")
	}
	d, _, err := maintenance.Unlock(path, oldMasterKey)
	if err != nil {
		t.Fatalf("right key: %v", err)
	}
	d.Close()
}
