package maintenance

// Keys of the database: how a start unlocks it, and how the master key
// changes (rotate-key, or once from the unsalted key of old installs).
//
//	andon.db ──Rekey──► andon.db.rekeyed (new file key)
//	                        │ every *_enc value re-encrypted in the copy
//	                        ▼
//	next start with the new key ──► copy swapped in (db.Open)
//
// The live file is never changed: restarting with the old key keeps
// working until the operator swaps the secret.

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"andon/internal/crypto"
	"andon/internal/db"
	"andon/internal/repos/misc"
)

// ErrWeakKey means a new master key could be guessed (see crypto.WeakKey).
var ErrWeakKey = errors.New("maintenance: master key too weak, use openssl rand -base64 32")

// ErrNoSalt means the database has no salt yet; start the server once,
// which adds it.
var ErrNoSalt = errors.New("maintenance: database has no key salt, start the server once first")

// ErrUnknownSealed names encrypted data rotation does not know how to
// re-encrypt; rotating anyway would lose it.
var ErrUnknownSealed = errors.New("maintenance: encrypted data of unknown purpose")

// Unlocked says what Unlock did.
type Unlocked string

const (
	UnlockedAsIs     Unlocked = "as_is"
	UnlockedNew      Unlocked = "new"      // a new database got its salt
	UnlockedUpgraded Unlocked = "upgraded" // an unsalted database moved to Argon2id
)

// Unlock derives the master key from secret and the salt next to the
// database at path, makes it the process key and opens the database. A
// database from before salts is re-encrypted under a fresh salt first:
// the salt file is written last, so a crash before it leaves the old
// database as it was.
func Unlock(path, secret string) (*sql.DB, Unlocked, error) {
	salt, err := db.ReadSalt(path)
	if err != nil {
		return nil, "", err
	}
	if salt != nil {
		d, err := open(path, crypto.Derive(secret, salt))
		return d, UnlockedAsIs, err
	}

	salt = crypto.NewSalt()
	if !db.Exists(path) {
		if err := db.WriteSalt(path, salt); err != nil {
			return nil, "", err
		}
		d, err := open(path, crypto.Derive(secret, salt))
		return d, UnlockedNew, err
	}

	old, err := open(path, crypto.Legacy(secret))
	if err != nil {
		return nil, "", err
	}
	m := crypto.Derive(secret, salt)
	_, err = rekeyTo(old, path, m)
	old.Close()
	if err != nil {
		return nil, "", err
	}
	if err := db.WriteSalt(path, salt); err != nil {
		return nil, "", err
	}
	d, err := open(path, m)
	return d, UnlockedUpgraded, err
}

// open makes m the process key and opens the database under its file key.
func open(path string, m []byte) (*sql.DB, error) {
	crypto.Init(m)
	fileKey, err := crypto.DatabaseKey(m)
	if err != nil {
		return nil, err
	}
	return db.Open(path, fileKey)
}

// RotateKey writes a copy of the database with every secret re-encrypted
// under a new master key; the next start with that key swaps it in.
// Returns the number of values re-encrypted. Writes after it are not in
// the copy, so restart right away. Webhook URLs change with the key.
func RotateKey(d *sql.DB, dbPath, newSecret string) (int, error) {
	if crypto.WeakKey(newSecret) {
		return 0, ErrWeakKey
	}
	salt, err := db.ReadSalt(dbPath)
	if err != nil {
		return 0, err
	}
	if salt == nil {
		return 0, ErrNoSalt
	}
	return rekeyTo(d, dbPath, crypto.Derive(newSecret, salt))
}

// rekeyTo writes the copy under master m (see the package drawing); a
// failed copy is removed.
func rekeyTo(d *sql.DB, path string, m []byte) (int, error) {
	fileKey, err := crypto.DatabaseKey(m)
	if err != nil {
		return 0, err
	}
	if err := db.Rekey(d, path, fileKey); err != nil {
		return 0, err
	}
	copyDB, err := db.OpenRekeyed(path, fileKey)
	if err != nil {
		return 0, errors.Join(err, db.DropRekeyed(path))
	}
	count, err := resealAll(copyDB, m)
	if err := errors.Join(err, copyDB.Close()); err != nil {
		return 0, errors.Join(err, db.DropRekeyed(path))
	}
	return count, nil
}

// sealedPurpose is the key purpose of each place holding ciphertext:
// *_enc columns, and JSON columns with *_enc keys inside.
var sealedPurpose = map[string]crypto.Purpose{
	"connections.secret_enc":       crypto.PurposeCredential,
	"connections.oauth_client_enc": crypto.PurposeCredential,
	"user_credentials.secret_enc":  crypto.PurposeCredential,
	"oauth_grants.grant_enc":       crypto.PurposeCredential,
	"users.totp_secret_enc":        crypto.PurposeTOTP,
	"notify_channels.url_enc":      crypto.PurposeNotify,
	"widgets.config":               crypto.PurposeCredential, // tile secrets
	"revisions.data":               crypto.PurposeCredential, // tile secrets of earlier versions
	"instance_settings.value":      crypto.PurposeSetting,    // OIDC client secret
}

// purposeOf is the purpose of a sealed column; an unknown one stops the
// rotation rather than leave data behind.
func purposeOf(c misc.Column) (crypto.Purpose, error) {
	p, ok := sealedPurpose[c.String()]
	if !ok {
		return "", fmt.Errorf("%w: %s", ErrUnknownSealed, c)
	}
	return p, nil
}

// resealAll re-encrypts every sealed value of d from the process key to
// master m, in one transaction.
func resealAll(d *sql.DB, m []byte) (int, error) {
	count := 0
	err := db.WithTx(d, func(tx *sql.Tx) error {
		cols, err := misc.SealedColumns(tx)
		if err != nil {
			return err
		}
		for _, c := range cols {
			p, err := purposeOf(c)
			if err != nil {
				return err
			}
			n, err := misc.Reseal(tx, c, func(blob []byte) ([]byte, error) { return crypto.Reseal(blob, p, m) })
			if err != nil {
				return err
			}
			count += n
		}

		jsons, err := misc.SealedJSON(tx)
		if err != nil {
			return err
		}
		for _, c := range jsons {
			p, err := purposeOf(c)
			if err != nil {
				return err
			}
			n, err := misc.RewriteJSON(tx, c, func(v any) (any, bool, error) { return resealJSON(v, p, m) })
			if err != nil {
				return err
			}
			count += n
		}
		return nil
	})
	return count, err
}

// sealedKey ends JSON keys holding base64 ciphertext: {"api_key_enc": "q3…"}.
const sealedKey = "_enc"

// resealJSON re-encrypts the *_enc values anywhere in v; changed tells
// whether it found one.
func resealJSON(v any, p crypto.Purpose, m []byte) (any, bool, error) {
	switch x := v.(type) {
	case map[string]any:
		changed := false
		for k, item := range x {
			text, ok := item.(string)
			if strings.HasSuffix(k, sealedKey) && ok && text != "" {
				blob, err := base64.StdEncoding.DecodeString(text)
				if err != nil {
					return nil, false, fmt.Errorf("%s: %w", k, err)
				}
				if blob, err = crypto.Reseal(blob, p, m); err != nil {
					return nil, false, fmt.Errorf("%s: %w", k, err)
				}
				x[k], changed = base64.StdEncoding.EncodeToString(blob), true
				continue
			}
			next, sub, err := resealJSON(item, p, m)
			if err != nil {
				return nil, false, err
			}
			x[k], changed = next, changed || sub
		}
		return x, changed, nil
	case []any:
		changed := false
		for i, item := range x {
			next, sub, err := resealJSON(item, p, m)
			if err != nil {
				return nil, false, err
			}
			x[i], changed = next, changed || sub
		}
		return x, changed, nil
	}
	return v, false, nil
}
