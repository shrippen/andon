package db

// The install's key salt lives next to the database file, in hex:
//
//	andon.db        encrypted under a key from MASTER_KEY and the salt
//	andon.db.salt   16 random bytes, not secret, but needed to open it
//
// A copy (backup, snapshot) gets its own .salt beside it, so it opens
// wherever it is restored. No salt means a database from before salts.

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const (
	saltSuffix = ".salt"
	fileMode   = 0o600
)

// ReadSalt returns the salt next to the database at path, nil when there
// is none.
func ReadSalt(path string) ([]byte, error) {
	raw, err := os.ReadFile(path + saltSuffix)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return hex.DecodeString(strings.TrimSpace(string(raw)))
}

// WriteSalt stores salt next to the database at path, durably: a synced
// temporary file renamed into place, then the directory synced. Once it
// returns, the next start derives the key with it.
func WriteSalt(path string, salt []byte) error {
	tmp := path + saltSuffix + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fileMode)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(hex.EncodeToString(salt) + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path+saltSuffix); err != nil {
		return err
	}
	return syncDir(filepath.Dir(path))
}

// syncDir makes a rename in dir durable.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// Exists tells whether a database file is at path.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// RemoveCopy deletes a copy of the database and its salt.
func RemoveCopy(path string) error {
	if err := removeIfExists(path + saltSuffix); err != nil {
		return err
	}
	return removeIfExists(path)
}

// OpenRekeyed opens the copy Rekey wrote at path, to finish it before
// the next start swaps it in.
func OpenRekeyed(path string, key []byte) (*sql.DB, error) {
	d, err := sql.Open(driverName, dsn(path+rekeyedSuffix, key, "&_txlock=immediate"))
	if err != nil {
		return nil, err
	}
	d.SetMaxOpenConns(1)
	return d, nil
}

// DropRekeyed removes a copy Rekey wrote, e.g. after a failed rotation.
func DropRekeyed(path string) error {
	return removeIfExists(path + rekeyedSuffix)
}
