//go:build release

package seed

import (
	"context"
	"database/sql"
	"errors"
)

// Demo is not part of release builds: no demo accounts, boards or data.
func Demo(context.Context, *sql.DB) error {
	return errors.New("seed: no demo mode in release builds")
}
