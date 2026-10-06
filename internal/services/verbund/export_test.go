package verbund

import (
	"database/sql"
	"time"

	"andon/internal/caps"
	linkrepo "andon/internal/repos/links"
)

// TestKey is a key for StoreEntryForTest; None stores "no counterpart".
type TestKey struct {
	ConnID int64
	Key    string
	None   bool
}

// StoreEntryForTest stores a customer entry as is, e.g. one an earlier
// version left (Kimai customer without a Ninja client).
func StoreEntryForTest(d *sql.DB, id int64, keys []TestKey) error {
	var list []linkrepo.Key
	for _, k := range keys {
		state := linkrepo.KeyConfirmed
		if k.None {
			state = linkrepo.KeyNone
		}
		list = append(list, linkrepo.Key{ConnID: k.ConnID, Key: k.Key, State: state})
	}
	_, err := linkrepo.PutEntry(d, id, string(caps.Customers), list, time.Now().UTC())
	return err
}
