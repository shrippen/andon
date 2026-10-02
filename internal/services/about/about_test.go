package about

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"andon/internal/db"
	"andon/internal/db/dbtest"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"), dbtest.Key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	return d
}

func TestGetStamped(t *testing.T) {
	version = "v1.2.3"
	t.Cleanup(func() { version = "" })

	info, err := Get(openDB(t))
	if err != nil {
		t.Fatal(err)
	}
	if info.Version != "v1.2.3" {
		t.Fatalf("version %q", info.Version)
	}
}

func TestGetSchema(t *testing.T) {
	info, err := Get(openDB(t))
	if err != nil {
		t.Fatal(err)
	}
	if info.Version == "" || info.Migrations == 0 || strings.HasSuffix(info.Schema, ".sql") || info.Schema == "" {
		t.Fatalf("info %+v", info)
	}
	if info.Started.IsZero() {
		t.Fatal("no start time")
	}
}
