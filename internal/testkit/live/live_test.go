package live

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"andon/internal/enums"
	"andon/internal/services/maintenance"
	"andon/internal/testkit"
)

// local points the package at an empty .local-test/ in a temp dir.
func local(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	t.Setenv(dirEnv, d)
	t.Setenv(runEnv, "1")
	return d
}

func TestTargetSkipsWithoutConfig(t *testing.T) {
	local(t)
	ran := false
	t.Run("kimai", func(t *testing.T) {
		Target(t, Kimai)
		ran = true
	})
	if ran {
		t.Fatal("test ran without a configured instance")
	}
}

func TestTargetSkipsUnasked(t *testing.T) {
	local(t)
	t.Setenv(runEnv, "")
	ran := false
	t.Run("kimai", func(t *testing.T) {
		Target(t, Kimai)
		ran = true
	})
	if ran {
		t.Fatal("live test ran without being asked for")
	}
}

func TestTargetReadsInstance(t *testing.T) {
	d := local(t)
	const key = "local-key"
	if err := os.MkdirAll(filepath.Join(d, filepath.Dir(keyPath)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, keyPath), []byte(key+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(d, filepath.Dir(dbPath)), 0o700); err != nil {
		t.Fatal(err)
	}

	// The local instance with one Kimai connection, token "tok".
	inst, _, err := maintenance.Unlock(filepath.Join(d, dbPath), key)
	if err != nil {
		t.Fatal(err)
	}
	who, space := testkit.User(t, inst, "a@b.c", enums.RoleAdmin)
	testkit.Conn(t, inst, who, space, enums.ServiceKimai, "https://kimai.example/")
	inst.Close()

	to := Target(t, Kimai)
	if to.URL != "https://kimai.example" || to.Token != "tok" || !to.VerifyTLS {
		t.Fatalf("target = %+v", to)
	}

	ran := false
	t.Run("paperless", func(t *testing.T) {
		Target(t, Paperless)
		ran = true
	})
	if ran {
		t.Fatal("test ran without a paperless connection")
	}
}

func TestChangeOnlyOwnEntries(t *testing.T) {
	d := local(t)
	Created(t, Kimai, "place", 7)
	Change(t, Kimai, Update, "place", 7)

	// Same id of another kind or service: not ours.
	for _, c := range []struct {
		svc  Service
		kind string
	}{{Kimai, "timesheet"}, {Dawarich, "place"}} {
		if own(t, c.svc, c.kind, 7) {
			t.Errorf("%s %s 7 counted as own", c.svc, c.kind)
		}
	}

	log, err := os.ReadFile(filepath.Join(d, logName))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(log)), "\n")
	if len(lines) != 2 || !strings.Contains(lines[1], "kimai\tupdate\tplace\t7\tTestChangeOnlyOwnEntries") {
		t.Fatalf("log = %q", log)
	}
}

func TestNameMarksTestEntries(t *testing.T) {
	if n := Name(t); !strings.HasPrefix(n, namePrefix+" TestNameMarksTestEntries ") {
		t.Fatalf("name = %q", n)
	}
}

func TestChangeTextIDs(t *testing.T) {
	local(t)
	Created(t, Ninja, "invoice", "Kx9")
	Change(t, Ninja, Delete, "invoice", "Kx9")
	if own(t, Ninja, "invoice", "Ab1") {
		t.Fatal("foreign key counted as own")
	}
}
