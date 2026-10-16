// Package about reports what build of Andon is running.
package about

import (
	"database/sql"
	"runtime"
	"runtime/debug"
	"strings"
	"time"

	"andon/internal/db"
)

const devVersion = "dev"

// version is set at build time:
//
//	go build -ldflags "-X andon/internal/services/about.version=v0.4.0"
var version string

// Version is the running build's version. Without a stamped version it
// falls back to the module version Go embeds, then to "dev".
func Version() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		return bi.Main.Version
	}
	return devVersion
}

// started is when the process came up.
var started = time.Now()

// Info describes the running build.
type Info struct {
	Version    string
	Go         string
	Started    time.Time
	Schema     string // newest migration, e.g. "0016_link_status_error"
	Migrations int
}

// Get returns the build and database info.
func Get(d *sql.DB) (Info, error) {
	info := Info{Version: Version(), Go: runtime.Version(), Started: started}
	err := db.WithRead(d, func(tx *sql.Tx) error {
		var err error
		if info.Schema, err = db.LastMigration(tx); err != nil {
			return err
		}
		info.Schema = strings.TrimSuffix(info.Schema, ".sql")
		info.Migrations, err = db.Migrations(tx)
		return err
	})

	return info, err
}
