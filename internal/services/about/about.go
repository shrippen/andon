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

// Get returns the build and database info. Without a stamped version it falls back to
// the module version Go embeds, then to "dev".
func Get(d *sql.DB) (Info, error) {
	v := version
	if v == "" {
		if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
			v = bi.Main.Version
		}
	}
	if v == "" {
		v = devVersion
	}

	info := Info{Version: v, Go: runtime.Version(), Started: started}
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
